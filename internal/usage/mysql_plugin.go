package usage

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// MySQLPluginConfig holds the configuration for MySQL persistence
type MySQLPluginConfig struct {
	DSN              string        // MySQL connection string
	BatchSize        int           // Number of records to batch before insert
	FlushInterval    time.Duration // Maximum time to wait before flushing batch
	MaxRetries       int           // Maximum retry attempts for failed inserts
	LoadHistoryDays  int           // Number of days of history to load on startup (0 = all)
	EnableDailyStats bool          // Whether to maintain daily aggregated stats
}

// maskAPIKey masks an API key for storage, preserving first 4 and last 4 characters
// Example: "sk-abc123xyz789" -> "sk-a****789"
func maskAPIKey(apiKey string) string {
	if len(apiKey) <= 8 {
		// Too short to mask meaningfully, return all masked
		return "****"
	}
	prefix := apiKey[:4]
	suffix := apiKey[len(apiKey)-4:]
	return prefix + "****" + suffix
}

// MySQLPlugin implements usage.Plugin to persist records to MySQL
type MySQLPlugin struct {
	db     *sql.DB
	config MySQLPluginConfig

	// Batch processing
	mu            sync.Mutex
	batch         []coreusage.Record
	batchContexts []context.Context
	flushTimer    *time.Timer

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool

	// Statistics
	stats *RequestStatistics
}

// NewMySQLPlugin creates a new MySQL persistence plugin
func NewMySQLPlugin(config MySQLPluginConfig, stats *RequestStatistics) (*MySQLPlugin, error) {
	if config.DSN == "" {
		return nil, fmt.Errorf("MySQL DSN is required")
	}

	// Set defaults
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 5 * time.Second
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	// Open database connection
	db, err := sql.Open("mysql", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open MySQL connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping MySQL: %w", err)
	}

	log.Infof("MySQL usage plugin connected to database")

	ctx, cancel = context.WithCancel(context.Background())
	plugin := &MySQLPlugin{
		db:     db,
		config: config,
		batch:  make([]coreusage.Record, 0, config.BatchSize),
		ctx:    ctx,
		cancel: cancel,
		stats:  stats,
	}

	// Start background flush worker
	plugin.wg.Add(1)
	go plugin.flushWorker()

	return plugin, nil
}

// HandleUsage implements coreusage.Plugin
func (p *MySQLPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	// Add to batch
	p.batch = append(p.batch, record)
	p.batchContexts = append(p.batchContexts, ctx)

	// Reset flush timer
	if p.flushTimer != nil {
		p.flushTimer.Stop()
	}
	p.flushTimer = time.AfterFunc(p.config.FlushInterval, func() {
		p.flush()
	})

	// Flush if batch is full
	if len(p.batch) >= p.config.BatchSize {
		p.flushLocked()
	}
}

// flush performs a batch insert (thread-safe)
func (p *MySQLPlugin) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushLocked()
}

// flushLocked performs a batch insert (caller must hold lock)
func (p *MySQLPlugin) flushLocked() {
	if len(p.batch) == 0 {
		return
	}

	batch := p.batch
	p.batch = make([]coreusage.Record, 0, p.config.BatchSize)
	p.batchContexts = nil

	// Release lock before executing database operation
	p.mu.Unlock()
	p.insertBatch(batch)
	p.mu.Lock()
}

// insertBatch inserts a batch of records with retry logic
func (p *MySQLPlugin) insertBatch(batch []coreusage.Record) {
	if len(batch) == 0 {
		return
	}

	var lastErr error
	for attempt := 0; attempt < p.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
			log.Warnf("MySQL usage plugin: retrying batch insert (attempt %d/%d)", attempt+1, p.config.MaxRetries)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := p.executeBatchInsert(ctx, batch)
		cancel()

		if err == nil {
			if attempt > 0 {
				log.Infof("MySQL usage plugin: batch insert succeeded after %d retries", attempt)
			}
			return
		}

		lastErr = err
		log.Errorf("MySQL usage plugin: batch insert failed (attempt %d/%d): %v", attempt+1, p.config.MaxRetries, err)
	}

	log.Errorf("MySQL usage plugin: failed to insert batch after %d attempts: %v", p.config.MaxRetries, lastErr)
}

// executeBatchInsert performs the actual batch insert
func (p *MySQLPlugin) executeBatchInsert(ctx context.Context, batch []coreusage.Record) error {
	if len(batch) == 0 {
		return nil
	}

	// Build batch insert query
	query := `INSERT INTO usage_records (
		provider, model, api_key, source, auth_id, auth_index,
		requested_at, failed,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens
	) VALUES `

	values := make([]interface{}, 0, len(batch)*13)
	placeholders := make([]string, 0, len(batch))

	for _, record := range batch {
		placeholders = append(placeholders, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		values = append(values,
			record.Provider,
			record.Model,
			maskAPIKey(record.APIKey),
			record.Source,
			record.AuthID,
			record.AuthIndex,
			record.RequestedAt,
			record.Failed,
			record.Detail.InputTokens,
			record.Detail.OutputTokens,
			record.Detail.ReasoningTokens,
			record.Detail.CachedTokens,
			record.Detail.TotalTokens,
		)
	}

	query += placeholders[0]
	for i := 1; i < len(placeholders); i++ {
		query += ", " + placeholders[i]
	}

	// Execute batch insert
	result, err := p.db.ExecContext(ctx, query, values...)
	if err != nil {
		return fmt.Errorf("batch insert failed: %w", err)
	}

	affected, _ := result.RowsAffected()
	log.Debugf("MySQL usage plugin: inserted %d records", affected)

	// Update daily stats if enabled
	if p.config.EnableDailyStats {
		go p.updateDailyStats(batch)
	}

	return nil
}

// updateDailyStats updates the aggregated daily statistics table
func (p *MySQLPlugin) updateDailyStats(batch []coreusage.Record) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, record := range batch {
		statDate := record.RequestedAt.Format("2006-01-02")

		query := `INSERT INTO usage_daily_stats (
			stat_date, api_key, model, provider,
			total_requests, success_requests, failed_requests,
			total_tokens, input_tokens, output_tokens, reasoning_tokens, cached_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			total_requests = total_requests + VALUES(total_requests),
			success_requests = success_requests + VALUES(success_requests),
			failed_requests = failed_requests + VALUES(failed_requests),
			total_tokens = total_tokens + VALUES(total_tokens),
			input_tokens = input_tokens + VALUES(input_tokens),
			output_tokens = output_tokens + VALUES(output_tokens),
			reasoning_tokens = reasoning_tokens + VALUES(reasoning_tokens),
			cached_tokens = cached_tokens + VALUES(cached_tokens),
			updated_at = NOW(3)`

		successReq := 0
		failedReq := 0
		if record.Failed {
			failedReq = 1
		} else {
			successReq = 1
		}

		_, err := p.db.ExecContext(ctx, query,
			statDate, maskAPIKey(record.APIKey), record.Model, record.Provider,
			1, successReq, failedReq,
			record.Detail.TotalTokens,
			record.Detail.InputTokens,
			record.Detail.OutputTokens,
			record.Detail.ReasoningTokens,
			record.Detail.CachedTokens,
		)

		if err != nil {
			log.Errorf("MySQL usage plugin: failed to update daily stats: %v", err)
		}
	}
}

// flushWorker runs in background to ensure periodic flushes
func (p *MySQLPlugin) flushWorker() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.flush()
		case <-p.ctx.Done():
			// Final flush before shutdown
			p.flush()
			return
		}
	}
}

// LoadHistoryIntoMemory loads historical data from MySQL into in-memory statistics
func (p *MySQLPlugin) LoadHistoryIntoMemory() error {
	if p.stats == nil {
		return fmt.Errorf("in-memory statistics not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	query := `SELECT
		provider, model, api_key, source, auth_id, auth_index,
		requested_at, failed,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens
	FROM usage_records`

	args := []interface{}{}
	if p.config.LoadHistoryDays > 0 {
		query += ` WHERE requested_at >= DATE_SUB(NOW(), INTERVAL ? DAY)`
		args = append(args, p.config.LoadHistoryDays)
	}

	query += ` ORDER BY requested_at ASC`

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to query history: %w", err)
	}
	defer rows.Close()

	loadedCount := 0
	for rows.Next() {
		var record coreusage.Record
		err := rows.Scan(
			&record.Provider,
			&record.Model,
			&record.APIKey,
			&record.Source,
			&record.AuthID,
			&record.AuthIndex,
			&record.RequestedAt,
			&record.Failed,
			&record.Detail.InputTokens,
			&record.Detail.OutputTokens,
			&record.Detail.ReasoningTokens,
			&record.Detail.CachedTokens,
			&record.Detail.TotalTokens,
		)
		if err != nil {
			log.Errorf("MySQL usage plugin: failed to scan row: %v", err)
			continue
		}

		// Convert to in-memory format and record
		p.stats.Record(context.Background(), record)
		loadedCount++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	log.Infof("MySQL usage plugin: loaded %d historical records into memory", loadedCount)
	return nil
}

// Close gracefully shuts down the plugin
func (p *MySQLPlugin) Close() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true

	// Stop flush timer
	if p.flushTimer != nil {
		p.flushTimer.Stop()
	}
	p.mu.Unlock()

	// Stop background worker
	p.cancel()
	p.wg.Wait()

	// Final flush
	p.flush()

	// Close database connection
	if p.db != nil {
		if err := p.db.Close(); err != nil {
			return fmt.Errorf("failed to close MySQL connection: %w", err)
		}
	}

	log.Info("MySQL usage plugin closed")
	return nil
}

// GetDailyStats retrieves aggregated daily statistics
func (p *MySQLPlugin) GetDailyStats(ctx context.Context, startDate, endDate time.Time) ([]DailyStats, error) {
	query := `SELECT
		stat_date, api_key, model, provider,
		total_requests, success_requests, failed_requests,
		total_tokens, input_tokens, output_tokens, reasoning_tokens, cached_tokens
	FROM usage_daily_stats
	WHERE stat_date >= ? AND stat_date <= ?
	ORDER BY stat_date DESC, total_tokens DESC`

	rows, err := p.db.QueryContext(ctx, query, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("failed to query daily stats: %w", err)
	}
	defer rows.Close()

	var results []DailyStats
	for rows.Next() {
		var stat DailyStats
		err := rows.Scan(
			&stat.Date,
			&stat.APIKey,
			&stat.Model,
			&stat.Provider,
			&stat.TotalRequests,
			&stat.SuccessRequests,
			&stat.FailedRequests,
			&stat.TotalTokens,
			&stat.InputTokens,
			&stat.OutputTokens,
			&stat.ReasoningTokens,
			&stat.CachedTokens,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, stat)
	}

	return results, rows.Err()
}

// DailyStats represents aggregated statistics for a day
type DailyStats struct {
	Date            string `json:"date"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	Provider        string `json:"provider"`
	TotalRequests   int64  `json:"total_requests"`
	SuccessRequests int64  `json:"success_requests"`
	FailedRequests  int64  `json:"failed_requests"`
	TotalTokens     int64  `json:"total_tokens"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ReasoningTokens int64  `json:"reasoning_tokens"`
	CachedTokens    int64  `json:"cached_tokens"`
}

// GetSnapshot retrieves a complete statistics snapshot from the database.
// This queries the database directly rather than returning in-memory data.
func (p *MySQLPlugin) GetSnapshot(ctx context.Context) (StatisticsSnapshot, error) {
	result := StatisticsSnapshot{
		APIs:           make(map[string]APISnapshot),
		RequestsByDay:  make(map[string]int64),
		RequestsByHour: make(map[string]int64),
		TokensByDay:    make(map[string]int64),
		TokensByHour:   make(map[string]int64),
	}

	if p == nil || p.db == nil {
		return result, fmt.Errorf("MySQL plugin not initialized")
	}

	// Query total statistics
	var totalRequests, successCount, failureCount, totalTokens int64
	err := p.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) as total_requests,
			SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN failed = 1 THEN 1 ELSE 0 END) as failure_count,
			COALESCE(SUM(total_tokens), 0) as total_tokens
		FROM usage_records
	`).Scan(&totalRequests, &successCount, &failureCount, &totalTokens)
	if err != nil {
		return result, fmt.Errorf("failed to query total stats: %w", err)
	}

	result.TotalRequests = totalRequests
	result.SuccessCount = successCount
	result.FailureCount = failureCount
	result.TotalTokens = totalTokens

	// Query statistics by API key and model
	rows, err := p.db.QueryContext(ctx, `
		SELECT
			api_key,
			model,
			COUNT(*) as total_requests,
			COALESCE(SUM(total_tokens), 0) as total_tokens
		FROM usage_records
		GROUP BY api_key, model
	`)
	if err != nil {
		return result, fmt.Errorf("failed to query API stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var apiKey, model string
		var reqCount, tokens int64
		if err := rows.Scan(&apiKey, &model, &reqCount, &tokens); err != nil {
			log.Errorf("MySQL plugin: failed to scan API stats row: %v", err)
			continue
		}

		apiSnapshot, exists := result.APIs[apiKey]
		if !exists {
			apiSnapshot = APISnapshot{
				Models: make(map[string]ModelSnapshot),
			}
		}
		apiSnapshot.TotalRequests += reqCount
		apiSnapshot.TotalTokens += tokens
		apiSnapshot.Models[model] = ModelSnapshot{
			TotalRequests: reqCount,
			TotalTokens:   tokens,
		}
		result.APIs[apiKey] = apiSnapshot
	}

	// Query requests by day
	dayRows, err := p.db.QueryContext(ctx, `
		SELECT
			DATE(requested_at) as day,
			COUNT(*) as requests,
			COALESCE(SUM(total_tokens), 0) as tokens
		FROM usage_records
		GROUP BY DATE(requested_at)
	`)
	if err != nil {
		return result, fmt.Errorf("failed to query daily stats: %w", err)
	}
	defer dayRows.Close()

	for dayRows.Next() {
		var day string
		var requests, tokens int64
		if err := dayRows.Scan(&day, &requests, &tokens); err != nil {
			log.Errorf("MySQL plugin: failed to scan daily stats row: %v", err)
			continue
		}
		result.RequestsByDay[day] = requests
		result.TokensByDay[day] = tokens
	}

	// Query requests by hour
	hourRows, err := p.db.QueryContext(ctx, `
		SELECT
			HOUR(requested_at) as hour,
			COUNT(*) as requests,
			COALESCE(SUM(total_tokens), 0) as tokens
		FROM usage_records
		GROUP BY HOUR(requested_at)
	`)
	if err != nil {
		return result, fmt.Errorf("failed to query hourly stats: %w", err)
	}
	defer hourRows.Close()

	for hourRows.Next() {
		var hour int
		var requests, tokens int64
		if err := hourRows.Scan(&hour, &requests, &tokens); err != nil {
			log.Errorf("MySQL plugin: failed to scan hourly stats row: %v", err)
			continue
		}
		hourKey := fmt.Sprintf("%02d", hour)
		result.RequestsByHour[hourKey] = requests
		result.TokensByHour[hourKey] = tokens
	}

	return result, nil
}
