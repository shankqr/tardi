-- +goose Up
CREATE TABLE snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vps_instance_id UUID NOT NULL REFERENCES vps_instances(id),
    provider_image_id TEXT,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'creating',
    size_gb REAL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_snapshots_instance ON snapshots(vps_instance_id);

-- +goose Down
DROP TABLE snapshots;
