-- +goose Up
CREATE TABLE incidents (
    id INTEGER PRIMARY KEY,
    service_id TEXT NOT NULL CHECK (service_id <> ''),
    started_at_us INTEGER NOT NULL,
    resolved_at_us INTEGER,
    CHECK (resolved_at_us IS NULL OR resolved_at_us > started_at_us)
);

CREATE UNIQUE INDEX idx_incidents_one_open_per_service
    ON incidents(service_id)
    WHERE resolved_at_us IS NULL;

-- +goose Down
DROP TABLE incidents;
