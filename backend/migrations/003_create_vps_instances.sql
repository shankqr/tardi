-- +goose Up
CREATE TABLE vps_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    subscription_id UUID NOT NULL REFERENCES subscriptions(id),
    provider TEXT NOT NULL,
    provider_server_id TEXT UNIQUE,
    provider_region TEXT,
    name TEXT NOT NULL,
    ipv4 INET,
    region TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'requested',
    agent_token_secret_name TEXT,
    last_heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_vps_instances_user ON vps_instances(user_id);

-- +goose Down
DROP TABLE vps_instances;
