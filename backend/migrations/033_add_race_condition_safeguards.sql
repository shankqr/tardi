-- +goose Up

-- RC-1: Enforce 1 active instance per user at the DB level.
-- Only one row per user_id can exist where status is not terminated/error.
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_active_instance_per_user
  ON vps_instances (user_id)
  WHERE status NOT IN ('terminated', 'error');

-- RC-7: Prevent duplicate subscriptions per user.
-- Only one row per user_id can exist where status is not canceled.
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_active_subscription_per_user
  ON subscriptions (user_id)
  WHERE status != 'canceled';

-- +goose Down
DROP INDEX IF EXISTS idx_one_active_instance_per_user;
DROP INDEX IF EXISTS idx_one_active_subscription_per_user;
