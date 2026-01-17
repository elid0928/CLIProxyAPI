-- MySQL Schema for CLIProxyAPI Usage Statistics
-- This schema stores detailed usage records for token consumption tracking

CREATE TABLE IF NOT EXISTS `usage_records` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `provider` VARCHAR(64) NOT NULL COMMENT 'Provider name (gemini, claude, codex, etc)',
  `model` VARCHAR(128) NOT NULL COMMENT 'Model name',
  `api_key` VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Client API key identifier',
  `source` VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Account email or project ID',
  `auth_id` VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Internal auth credential ID',
  `auth_index` VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Auth credential index',

  `requested_at` DATETIME(3) NOT NULL COMMENT 'Request timestamp with millisecond precision',
  `failed` TINYINT(1) NOT NULL DEFAULT 0 COMMENT 'Whether the request failed',

  `input_tokens` BIGINT NOT NULL DEFAULT 0 COMMENT 'Input/prompt tokens',
  `output_tokens` BIGINT NOT NULL DEFAULT 0 COMMENT 'Output/completion tokens',
  `reasoning_tokens` BIGINT NOT NULL DEFAULT 0 COMMENT 'Reasoning tokens (o1/thinking mode)',
  `cached_tokens` BIGINT NOT NULL DEFAULT 0 COMMENT 'Cached tokens (Claude prompt caching)',
  `total_tokens` BIGINT NOT NULL DEFAULT 0 COMMENT 'Total token count',

  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  PRIMARY KEY (`id`),

  -- Index for time-range queries
  KEY `idx_requested_at` (`requested_at`),

  -- Index for filtering by API key
  KEY `idx_api_key` (`api_key`, `requested_at`),

  -- Index for filtering by model
  KEY `idx_model` (`model`, `requested_at`),

  -- Index for filtering by provider
  KEY `idx_provider` (`provider`, `requested_at`),

  -- Index for source-based queries (account tracking)
  KEY `idx_source` (`source`, `requested_at`),

  -- Composite index for common aggregation queries
  KEY `idx_composite` (`api_key`, `model`, `requested_at`)

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Usage statistics records with token consumption details';

-- Create aggregated statistics table for faster queries (optional optimization)
CREATE TABLE IF NOT EXISTS `usage_daily_stats` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `stat_date` DATE NOT NULL COMMENT 'Statistics date',
  `api_key` VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Client API key identifier',
  `model` VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Model name',
  `provider` VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Provider name',

  `total_requests` BIGINT NOT NULL DEFAULT 0,
  `success_requests` BIGINT NOT NULL DEFAULT 0,
  `failed_requests` BIGINT NOT NULL DEFAULT 0,

  `total_tokens` BIGINT NOT NULL DEFAULT 0,
  `input_tokens` BIGINT NOT NULL DEFAULT 0,
  `output_tokens` BIGINT NOT NULL DEFAULT 0,
  `reasoning_tokens` BIGINT NOT NULL DEFAULT 0,
  `cached_tokens` BIGINT NOT NULL DEFAULT 0,

  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

  PRIMARY KEY (`id`),

  -- Unique constraint to prevent duplicate daily stats
  UNIQUE KEY `uk_daily_stats` (`stat_date`, `api_key`, `model`, `provider`),

  KEY `idx_date` (`stat_date`)

) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Pre-aggregated daily statistics for performance';

-- Useful queries for reporting

-- Total tokens by model (last 7 days)
-- SELECT model, SUM(total_tokens) as tokens, COUNT(*) as requests
-- FROM usage_records
-- WHERE requested_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
-- GROUP BY model
-- ORDER BY tokens DESC;

-- Daily token consumption
-- SELECT DATE(requested_at) as date, SUM(total_tokens) as tokens
-- FROM usage_records
-- WHERE requested_at >= DATE_SUB(NOW(), INTERVAL 30 DAY)
-- GROUP BY DATE(requested_at)
-- ORDER BY date;

-- Top API keys by usage
-- SELECT api_key, COUNT(*) as requests, SUM(total_tokens) as tokens
-- FROM usage_records
-- WHERE requested_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
-- GROUP BY api_key
-- ORDER BY tokens DESC
-- LIMIT 10;
