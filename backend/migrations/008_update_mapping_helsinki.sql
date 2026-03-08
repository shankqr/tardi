-- +goose Up
UPDATE provider_plan_mappings
SET provider_region = 'hel1'
WHERE provider = 'hetzner' AND provider_region = 'fsn1';

-- +goose Down
UPDATE provider_plan_mappings
SET provider_region = 'fsn1'
WHERE provider = 'hetzner' AND provider_region = 'hel1';
