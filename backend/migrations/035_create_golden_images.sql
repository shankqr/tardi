-- +goose Up
CREATE TABLE golden_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    server_type TEXT NOT NULL,
    provider_image_id TEXT NOT NULL,
    openclaw_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'building',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    deprecated_at TIMESTAMPTZ
);

CREATE INDEX idx_golden_images_active ON golden_images(provider, region, status) WHERE status = 'active';

-- +goose Down
DROP TABLE IF EXISTS golden_images;
