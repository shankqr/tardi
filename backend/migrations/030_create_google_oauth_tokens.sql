-- +goose Up
CREATE TABLE google_oauth_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    google_email TEXT NOT NULL,
    access_token_enc BYTEA NOT NULL,
    refresh_token_enc BYTEA NOT NULL,
    token_expiry TIMESTAMPTZ NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    revoked BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);

CREATE INDEX idx_google_oauth_tokens_expiry ON google_oauth_tokens (token_expiry)
    WHERE revoked = false;

-- +goose Down
DROP TABLE IF EXISTS google_oauth_tokens;
