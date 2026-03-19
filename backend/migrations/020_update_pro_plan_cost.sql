-- +goose Up
UPDATE provider_plan_mappings
SET monthly_cost_cents = 6500
WHERE plan_tier = 'pro' AND provider = 'hetzner';

-- +goose Down
UPDATE provider_plan_mappings
SET monthly_cost_cents = 4500
WHERE plan_tier = 'pro' AND provider = 'hetzner';
