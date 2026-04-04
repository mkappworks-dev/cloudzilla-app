ALTER TABLE webhook_deliveries
    ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_retry
    ON webhook_deliveries(next_retry_at)
    WHERE next_retry_at IS NOT NULL AND attempt_count < 5;
