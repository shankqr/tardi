-- +goose Up
ALTER TABLE subscriptions ADD COLUMN cancel_at_period_end BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN cancel_at_period_end;
