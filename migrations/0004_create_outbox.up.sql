-- Transactional outbox: events are inserted here in the SAME transaction as the
-- aggregate change, then relayed to the bus/broker by the application relay.
CREATE TABLE IF NOT EXISTS outbox (
    id           UUID PRIMARY KEY,
    event_name   TEXT        NOT NULL,
    aggregate_id UUID,
    payload      JSONB       NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ
);

-- The relay scans for unpublished rows; a partial index keeps that scan cheap.
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished
    ON outbox (occurred_at)
    WHERE published_at IS NULL;
