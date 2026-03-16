-- +goose Up
INSERT INTO provider_plan_mappings (plan_tier, provider, region, provider_server_type, provider_region, provider_image, monthly_cost_cents, is_available)
VALUES ('pro', 'hetzner', 'eu-central', 'ccx23', 'hel1', 'ubuntu-24.04', 4500, true);

-- +goose Down
DELETE FROM provider_plan_mappings WHERE plan_tier = 'pro';
