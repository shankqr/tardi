-- +goose Up
-- Country-level deploy choices for Hetzner locations that offer the CX23
-- Standard package. Pro keeps the existing dedicated-CPU CCX23 package in
-- the same selectable countries.
INSERT INTO provider_plan_mappings (
    plan_tier,
    provider,
    region,
    provider_server_type,
    provider_region,
    provider_image,
    monthly_cost_cents,
    is_available
)
SELECT
    v.plan_tier,
    v.provider,
    v.region,
    v.provider_server_type,
    v.provider_region,
    v.provider_image,
    v.monthly_cost_cents,
    v.is_available
FROM (VALUES
    ('standard', 'hetzner', 'fi', 'cx23',  'hel1', 'ubuntu-24.04', 349,  true),
    ('standard', 'hetzner', 'de', 'cx23',  'nbg1', 'ubuntu-24.04', 349,  true),
    ('pro',      'hetzner', 'fi', 'ccx23', 'hel1', 'ubuntu-24.04', 4500, true),
    ('pro',      'hetzner', 'de', 'ccx23', 'nbg1', 'ubuntu-24.04', 4500, true)
) AS v(plan_tier, provider, region, provider_server_type, provider_region, provider_image, monthly_cost_cents, is_available)
WHERE NOT EXISTS (
    SELECT 1
    FROM provider_plan_mappings existing
    WHERE existing.plan_tier = v.plan_tier
      AND existing.provider = v.provider
      AND existing.region = v.region
      AND existing.provider_server_type = v.provider_server_type
      AND existing.provider_region = v.provider_region
);

-- +goose Down
DELETE FROM provider_plan_mappings
WHERE provider = 'hetzner'
  AND region IN ('fi', 'de')
  AND (
    (plan_tier = 'standard' AND provider_server_type = 'cx23' AND provider_region IN ('hel1', 'nbg1'))
    OR
    (plan_tier = 'pro' AND provider_server_type = 'ccx23' AND provider_region IN ('hel1', 'nbg1'))
  );
