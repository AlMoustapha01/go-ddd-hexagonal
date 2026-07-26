-- CQRS read model for orders: a denormalized projection table, separate from
-- the write-side "orders" table, kept in sync by the application projector.
CREATE TABLE IF NOT EXISTS order_read (
    id          UUID PRIMARY KEY,
    customer_id UUID        NOT NULL,
    status      TEXT        NOT NULL,
    currency    CHAR(3)     NOT NULL,
    total_minor BIGINT      NOT NULL DEFAULT 0,
    item_count  INTEGER     NOT NULL DEFAULT 0,
    lines       JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_read_customer ON order_read (customer_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_order_read_updated ON order_read (updated_at DESC);
