-- +goose Up
CREATE TABLE incidents (
    id INTEGER PRIMARY KEY,
    service_id TEXT NOT NULL CHECK (service_id <> ''),
    started_at_ms INTEGER NOT NULL,
    resolved_at_ms INTEGER,
    CHECK (resolved_at_ms IS NULL OR resolved_at_ms > started_at_ms)
);

CREATE UNIQUE INDEX idx_incidents_one_open_per_service
    ON incidents(service_id)
    WHERE resolved_at_ms IS NULL;

-- +goose Down
DROP TABLE incidents;
