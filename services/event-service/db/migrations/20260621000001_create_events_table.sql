-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS events (
    event_id UUID PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    category VARCHAR(100),
    venue VARCHAR(255),
    city VARCHAR(100),
    event_date TIMESTAMPTZ NOT NULL,
    price NUMERIC(10, 2) NOT NULL,
    total_seats INT NOT NULL CHECK (total_seats >= 0),
    available_seats INT NOT NULL CHECK (available_seats >= 0),
    created_by UUID NOT NULL,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_events_category ON events (category);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_events_city ON events (city);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_events_event_date ON events (event_date);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_events_fulltext ON events USING GIN (
    to_tsvector(
        'english',
        title || ' ' || coalesce(description, '')
    )
);
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS events;
-- +goose StatementEnd