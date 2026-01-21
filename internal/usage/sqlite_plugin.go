package usage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// SQLitePluginConfig holds the configuration for SQLite persistence
type SQLitePluginConfig struct {
	DBPath           string        // Path to SQLite database file
	BatchSize        int           // Number of records to batch before insert
	FlushInterval    time.Duration // Maximum time to wait before flushing batch
	MaxRetries       int           // Maximum retry attempts for failed inserts
	LoadHistoryDays  int           // Number of days of history to load on startup (0 = all)
	EnableDailyStats bool          // Whether to maintain daily aggregated stats
}

// SQLitePlugin implements usage.Plugin to persist records to SQLite
type SQLitePlugin struct {
	db     *sql.DB
	config SQLitePluginConfig

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

// NewSQLitePlugin creates a new SQLite persistence plugin
func NewSQLitePlugin(config SQLitePluginConfig, stats *RequestStatistics) (*SQLitePlugin, error) {
	if config.DBPath == "" {
		return nil, fmt.Errorf("SQLite database path is required")
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

	// Expand ~ to home directory
	if config.DBPath[:2] == "~/" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		config.DBPath = filepath.Join(homeDir, config.DBPath[2:])
	}

	// Create directory if it doesn't exist
	dbDir := filepath.Dir(config.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite3", config.DBPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_cache_size=1000000000")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite works best with single writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // No limit

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping SQLite: %w", err)
	}

	// Initialize schema
	if err := initializeSQLiteSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Infof("SQLite usage plugin connected to database: %s", config.DBPath)

	ctx, cancel = context.WithCancel(context.Background())
	plugin := &SQLitePlugin{
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

// initializeSQLiteSchema creates tables and indexes if they don't exist
func initializeSQLiteSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS usage_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		api_key TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		auth_id TEXT NOT NULL DEFAULT '',
		auth_index TEXT NOT NULL DEFAULT '',
		requested_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		failed INTEGER NOT NULL DEFAULT 0,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_requested_at ON usage_records(requested_at);
	CREATE INDEX IF NOT EXISTS idx_api_key ON usage_records(api_key);
	CREATE INDEX IF NOT EXISTS idx_model ON usage_records(model);
	CREATE INDEX IF NOT EXISTS idx_provider ON usage_records(provider);
	CREATE INDEX IF NOT EXISTS idx_failed ON usage_records(failed);
	CREATE INDEX IF NOT EXISTS idx_composite_date_model ON usage_records(requested_at, model);
	CREATE INDEX IF NOT EXISTS idx_composite_api_model ON usage_records(api_key, model, requested_at);

	CREATE TABLE IF NOT EXISTS usage_daily_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stat_date TEXT NOT NULL,
		api_key TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		provider TEXT NOT NULL DEFAULT '',
		total_requests INTEGER NOT NULL DEFAULT 0,
		success_requests INTEGER NOT NULL DEFAULT 0,
		failed_requests INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_stats_unique
	ON usage_daily_stats(stat_date, api_key, model, provider);

	CREATE INDEX IF NOT EXISTS idx_daily_stats_date ON usage_daily_stats(stat_date);
	`

	_, err := db.Exec(schema)
	return err
}

// HandleUsage implements coreusage.Plugin
func (p *SQLitePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
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
func (p *SQLitePlugin) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushLocked()
}

// flushLocked performs a batch insert (caller must hold lock)
func (p *SQLitePlugin) flushLocked() {
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
func (p *SQLitePlugin) insertBatch(batch []coreusage.Record) {
	if len(batch) == 0 {
		return
	}

	var lastErr error
	for attempt := 0; attempt < p.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
			log.Warnf("SQLite usage plugin: retrying batch insert (attempt %d/%d)", attempt+1, p.config.MaxRetries)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := p.executeBatchInsert(ctx, batch)
		cancel()

		if err == nil {
			if attempt > 0 {
				log.Infof("SQLite usage plugin: batch insert succeeded after %d retries", attempt)
			}
			return
		}

		lastErr = err
		log.Errorf("SQLite usage plugin: batch insert failed (attempt %d/%d): %v", attempt+1, p.config.MaxRetries, err)
	}

	log.Errorf("SQLite usage plugin: failed to insert batch after %d attempts: %v", p.config.MaxRetries, lastErr)
}

// executeBatchInsert performs the actual batch insert
func (p *SQLitePlugin) executeBatchInsert(ctx context.Context, batch []coreusage.Record) error {
	if len(batch) == 0 {
		return nil
	}

	// Start transaction for better performance
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare statement
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO usage_records (
		provider, model, api_key, source, auth_id, auth_index,
		requested_at, failed,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Insert all records
	for _, record := range batch {
		_, err := stmt.ExecContext(ctx,
			record.Provider,
			record.Model,
			record.APIKey,
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
		if err != nil {
			return fmt.Errorf("failed to insert record: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Debugf("SQLite usage plugin: inserted %d records", len(batch))

	// Update daily stats if enabled
	if p.config.EnableDailyStats {
		go p.updateDailyStats(batch)
	}

	return nil
}

// updateDailyStats updates the aggregated daily statistics table
func (p *SQLitePlugin) updateDailyStats(batch []coreusage.Record) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, record := range batch {
		statDate := record.RequestedAt.Format("2006-01-02")

		query := `INSERT INTO usage_daily_stats (
			stat_date, api_key, model, provider,
			total_requests, success_requests, failed_requests,
			total_tokens, input_tokens, output_tokens, reasoning_tokens, cached_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(stat_date, api_key, model, provider) DO UPDATE SET
			total_requests = total_requests + excluded.total_requests,
			success_requests = success_requests + excluded.success_requests,
			failed_requests = failed_requests + excluded.failed_requests,
			total_tokens = total_tokens + excluded.total_tokens,
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
			cached_tokens = cached_tokens + excluded.cached_tokens,
			updated_at = CURRENT_TIMESTAMP`

		successReq := 0
		failedReq := 0
		if record.Failed {
			failedReq = 1
		} else {
			successReq = 1
		}

		_, err := p.db.ExecContext(ctx, query,
			statDate, record.APIKey, record.Model, record.Provider,
			1, successReq, failedReq,
			record.Detail.TotalTokens,
			record.Detail.InputTokens,
			record.Detail.OutputTokens,
			record.Detail.ReasoningTokens,
			record.Detail.CachedTokens,
		)

		if err != nil {
			log.Errorf("SQLite usage plugin: failed to update daily stats: %v", err)
		}
	}
}

// flushWorker runs in background to ensure periodic flushes
func (p *SQLitePlugin) flushWorker() {
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

// LoadHistoryIntoMemory loads historical data from SQLite into in-memory statistics
func (p *SQLitePlugin) LoadHistoryIntoMemory() error {
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
		query += ` WHERE requested_at >= datetime('now', '-' || ? || ' days')`
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
		var requestedAt string
		err := rows.Scan(
			&record.Provider,
			&record.Model,
			&record.APIKey,
			&record.Source,
			&record.AuthID,
			&record.AuthIndex,
			&requestedAt,
			&record.Failed,
			&record.Detail.InputTokens,
			&record.Detail.OutputTokens,
			&record.Detail.ReasoningTokens,
			&record.Detail.CachedTokens,
			&record.Detail.TotalTokens,
		)
		if err != nil {
			log.Errorf("SQLite usage plugin: failed to scan row: %v", err)
			continue
		}

		// Parse timestamp
		record.RequestedAt, err = time.Parse("2006-01-02 15:04:05", requestedAt)
		if err != nil {
			// Try alternative format
			record.RequestedAt, err = time.Parse(time.RFC3339, requestedAt)
			if err != nil {
				log.Errorf("SQLite usage plugin: failed to parse timestamp: %v", err)
				continue
			}
		}

		// Convert to in-memory format and record
		p.stats.Record(context.Background(), record)
		loadedCount++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	log.Infof("SQLite usage plugin: loaded %d historical records into memory", loadedCount)
	return nil
}

// Close gracefully shuts down the plugin
func (p *SQLitePlugin) Close() error {
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
			return fmt.Errorf("failed to close SQLite connection: %w", err)
		}
	}

	log.Info("SQLite usage plugin closed")
	return nil
}

// GetSnapshot retrieves a complete statistics snapshot from the database.
// This queries the database directly rather than returning in-memory data.
func (p *SQLitePlugin) GetSnapshot(ctx context.Context) (StatisticsSnapshot, error) {
	result := StatisticsSnapshot{
		APIs:           make(map[string]APISnapshot),
		RequestsByDay:  make(map[string]int64),
		RequestsByHour: make(map[string]int64),
		TokensByDay:    make(map[string]int64),
		TokensByHour:   make(map[string]int64),
	}

	if p == nil || p.db == nil {
		return result, fmt.Errorf("SQLite plugin not initialized")
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
			log.Errorf("SQLite plugin: failed to scan API stats row: %v", err)
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
			log.Errorf("SQLite plugin: failed to scan daily stats row: %v", err)
			continue
		}
		result.RequestsByDay[day] = requests
		result.TokensByDay[day] = tokens
	}

	// Query requests by hour
	hourRows, err := p.db.QueryContext(ctx, `
		SELECT
			CAST(strftime('%H', requested_at) AS INTEGER) as hour,
			COUNT(*) as requests,
			COALESCE(SUM(total_tokens), 0) as tokens
		FROM usage_records
		GROUP BY strftime('%H', requested_at)
	`)
	if err != nil {
		return result, fmt.Errorf("failed to query hourly stats: %w", err)
	}
	defer hourRows.Close()

	for hourRows.Next() {
		var hour int
		var requests, tokens int64
		if err := hourRows.Scan(&hour, &requests, &tokens); err != nil {
			log.Errorf("SQLite plugin: failed to scan hourly stats row: %v", err)
			continue
		}
		hourKey := fmt.Sprintf("%02d", hour)
		result.RequestsByHour[hourKey] = requests
		result.TokensByHour[hourKey] = tokens
	}

	return result, nil
}

// GetDailyStats retrieves aggregated daily statistics
func (p *SQLitePlugin) GetDailyStats(ctx context.Context, startDate, endDate time.Time) ([]DailyStats, error) {
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
