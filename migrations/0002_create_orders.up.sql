CREATE TABLE IF NOT EXISTS orders (
    id          UUID PRIMARY KEY,
    customer_id UUID        NOT NULL,
    currency    CHAR(3)     NOT NULL,
    status      TEXT        NOT NULL,
    lines       JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orders_customer_id ON orders (customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders (created_at DESC);
