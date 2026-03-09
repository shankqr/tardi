-- +goose Up
UPDATE provider_plan_mappings
SET provider_server_type = 'cx23', monthly_cost_cents = 349
WHERE provider = 'hetzner' AND provider_server_type = 'cx22';

-- +goose Down
UPDATE provider_plan_mappings
SET provider_server_type = 'cx22', monthly_cost_cents = 499
WHERE provider = 'hetzner' AND provider_server_type = 'cx23';
