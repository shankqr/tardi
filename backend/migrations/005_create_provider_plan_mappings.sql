-- +goose Up
CREATE TABLE provider_plan_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_tier TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    provider_server_type TEXT NOT NULL,
    provider_region TEXT NOT NULL,
    provider_image TEXT NOT NULL,
    monthly_cost_cents INT NOT NULL,
    is_available BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_provider_plan_mappings_lookup ON provider_plan_mappings(plan_tier, region, is_available);

INSERT INTO provider_plan_mappings (plan_tier, provider, region, provider_server_type, provider_region, provider_image, monthly_cost_cents, is_available)
VALUES ('standard', 'hetzner', 'eu-central', 'cx22', 'fsn1', 'ubuntu-24.04', 499, true);

-- +goose Down
DROP TABLE provider_plan_mappings;
