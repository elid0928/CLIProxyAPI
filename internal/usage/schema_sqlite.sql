-- SQLite schema for usage statistics persistence
-- This schema provides the same functionality as the MySQL version but using SQLite syntax

-- Main usage records table
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

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_requested_at ON usage_records(requested_at);
CREATE INDEX IF NOT EXISTS idx_api_key ON usage_records(api_key);
CREATE INDEX IF NOT EXISTS idx_model ON usage_records(model);
CREATE INDEX IF NOT EXISTS idx_provider ON usage_records(provider);
CREATE INDEX IF NOT EXISTS idx_failed ON usage_records(failed);
CREATE INDEX IF NOT EXISTS idx_composite_date_model ON usage_records(requested_at, model);
CREATE INDEX IF NOT EXISTS idx_composite_api_model ON usage_records(api_key, model, requested_at);

-- Daily aggregated statistics table (optional but recommended for performance)
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

-- Unique constraint for daily stats (prevents duplicates)
CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_stats_unique
ON usage_daily_stats(stat_date, api_key, model, provider);

-- Index for daily stats queries
CREATE INDEX IF NOT EXISTS idx_daily_stats_date ON usage_daily_stats(stat_date);

-- Helpful query examples (commented for reference)

-- Query 1: Token usage by model (last 7 days)
-- SELECT
--     model,
--     COUNT(*) as requests,
--     SUM(total_tokens) as total_tokens,
--     SUM(input_tokens) as input_tokens,
--     SUM(output_tokens) as output_tokens,
--     SUM(reasoning_tokens) as reasoning_tokens
-- FROM usage_records
-- WHERE requested_at >= datetime('now', '-7 days')
-- GROUP BY model
-- ORDER BY total_tokens DESC;

-- Query 2: Daily token consumption
-- SELECT
--     DATE(requested_at) as date,
--     COUNT(*) as requests,
--     SUM(total_tokens) as tokens,
--     ROUND(CAST(SUM(total_tokens) AS REAL) / COUNT(*), 2) as avg_tokens_per_request
-- FROM usage_records
-- WHERE requested_at >= datetime('now', '-30 days')
-- GROUP BY DATE(requested_at)
-- ORDER BY date DESC;

-- Query 3: Top API keys by usage
-- SELECT
--     api_key,
--     COUNT(*) as requests,
--     SUM(total_tokens) as tokens,
--     SUM(CASE WHEN failed = 1 THEN 1 ELSE 0 END) as failed_requests
-- FROM usage_records
-- WHERE requested_at >= datetime('now', '-7 days')
-- GROUP BY api_key
-- ORDER BY tokens DESC
-- LIMIT 10;

-- Query 4: Hourly traffic pattern (last 24 hours)
-- SELECT
--     strftime('%H', requested_at) as hour,
--     COUNT(*) as requests,
--     SUM(total_tokens) as tokens
-- FROM usage_records
-- WHERE requested_at >= datetime('now', '-1 day')
-- GROUP BY strftime('%H', requested_at)
-- ORDER BY hour;

-- Query 5: Success rate by model
-- SELECT
--     model,
--     COUNT(*) as total_requests,
--     SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END) as success_count,
--     SUM(CASE WHEN failed = 1 THEN 1 ELSE 0 END) as failed_count,
--     ROUND(100.0 * SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate
-- FROM usage_records
-- WHERE requested_at >= datetime('now', '-7 days')
-- GROUP BY model
-- ORDER BY total_requests DESC;
