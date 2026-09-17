-- +goose Up
CREATE TABLE check_results (
    id INTEGER PRIMARY KEY,
    service_id TEXT NOT NULL CHECK (service_id <> ''),
    checked_at_ms INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL CHECK (duration_ms >= 0),
    status_code INTEGER,
    error_kind TEXT NOT NULL,
    error_message TEXT
);

-- +goose Down
DROP TABLE check_results;
