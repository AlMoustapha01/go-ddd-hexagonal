-- Event store for the event-sourced Account aggregate. Append-only: rows are
-- never updated or deleted. The current state is derived by replaying a
-- stream in version order.
CREATE TABLE IF NOT EXISTS account_events (
    aggregate_id UUID        NOT NULL,
    version      INTEGER     NOT NULL,
    event_name   TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    -- The composite key gives optimistic concurrency for free: two writers
    -- cannot both insert the same next version.
    PRIMARY KEY (aggregate_id, version)
);
