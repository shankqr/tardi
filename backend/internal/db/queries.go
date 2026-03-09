package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/models"
)

var ErrNotFound = errors.New("not found")

// UpsertUser creates a user if they don't exist, or returns the existing one.
func UpsertUser(ctx context.Context, pool *pgxpool.Pool, firebaseUID, email string, name *string) (*models.User, error) {
	u := &models.User{}
	err := pool.QueryRow(ctx, `
		INSERT INTO users (firebase_uid, email, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (firebase_uid) DO UPDATE SET
			email = EXCLUDED.email,
			updated_at = now()
		RETURNING id, firebase_uid, email, name, created_at, updated_at
	`, firebaseUID, email, name).Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

// GetUserByFirebaseUID returns a user by their Firebase UID.
func GetUserByFirebaseUID(ctx context.Context, pool *pgxpool.Pool, firebaseUID string) (*models.User, error) {
	u := &models.User{}
	err := pool.QueryRow(ctx, `
		SELECT id, firebase_uid, email, name, created_at, updated_at
		FROM users WHERE firebase_uid = $1
	`, firebaseUID).Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by firebase uid: %w", err)
	}
	return u, nil
}

// GetSubscriptionByUserID returns the user's active subscription.
func GetSubscriptionByUserID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*models.Subscription, error) {
	s := &models.Subscription{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, stripe_subscription_id, stripe_customer_id,
		       plan_tier, status, current_period_end, created_at, updated_at
		FROM subscriptions WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 1
	`, userID).Scan(
		&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
		&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return s, nil
}

// CreateSubscription inserts a new subscription.
func CreateSubscription(ctx context.Context, pool *pgxpool.Pool, sub *models.Subscription) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO subscriptions (id, user_id, stripe_subscription_id, stripe_customer_id, plan_tier, status, current_period_end)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, sub.ID, sub.UserID, sub.StripeSubscriptionID, sub.StripeCustomerID,
		sub.PlanTier, sub.Status, sub.CurrentPeriodEnd)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return nil
}

// UpdateSubscriptionStatus updates a subscription's status and period end.
func UpdateSubscriptionStatus(ctx context.Context, pool *pgxpool.Pool, stripeSubID string, status models.SubscriptionStatus, periodEnd *time.Time) error {
	_, err := pool.Exec(ctx, `
		UPDATE subscriptions SET status = $1, current_period_end = $2, updated_at = now()
		WHERE stripe_subscription_id = $3
	`, status, periodEnd, stripeSubID)
	if err != nil {
		return fmt.Errorf("update subscription status: %w", err)
	}
	return nil
}

// GetSubscriptionByStripeSubID returns the subscription for a Stripe subscription ID.
func GetSubscriptionByStripeSubID(ctx context.Context, pool *pgxpool.Pool, stripeSubID string) (*models.Subscription, error) {
	s := &models.Subscription{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, stripe_subscription_id, stripe_customer_id,
		       plan_tier, status, current_period_end, created_at, updated_at
		FROM subscriptions WHERE stripe_subscription_id = $1
	`, stripeSubID).Scan(
		&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
		&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription by stripe sub id: %w", err)
	}
	return s, nil
}

// GetInstancesByUserID returns all non-terminated instances for a user.
func GetInstancesByUserID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]models.VpsInstance, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, subscription_id, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       (SELECT step FROM provisioning_jobs WHERE vps_instance_id = v.id AND status IN ('pending','running') LIMIT 1),
		       root_password, agent_token_secret_name, last_heartbeat_at, created_at, updated_at
		FROM vps_instances v
		WHERE user_id = $1 AND status != 'terminated'
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get instances: %w", err)
	}
	defer rows.Close()

	var instances []models.VpsInstance
	for rows.Next() {
		var inst models.VpsInstance
		if err := rows.Scan(
			&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Provider,
			&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
			&inst.Region, &inst.Status, &inst.Step,
			&inst.RootPassword, &inst.AgentTokenSecretName, &inst.LastHeartbeatAt,
			&inst.CreatedAt, &inst.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

// GetInstanceByID returns a single instance, scoped by user.
func GetInstanceByID(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, userID uuid.UUID) (*models.VpsInstance, error) {
	inst := &models.VpsInstance{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, subscription_id, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       (SELECT step FROM provisioning_jobs WHERE vps_instance_id = v.id AND status IN ('pending','running') LIMIT 1),
		       root_password, agent_token_secret_name, last_heartbeat_at, created_at, updated_at
		FROM vps_instances v
		WHERE id = $1 AND user_id = $2
	`, instanceID, userID).Scan(
		&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Provider,
		&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
		&inst.Region, &inst.Status, &inst.Step,
		&inst.RootPassword, &inst.AgentTokenSecretName, &inst.LastHeartbeatAt,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get instance by id: %w", err)
	}
	return inst, nil
}

// CreateInstance inserts a new VPS instance.
func CreateInstance(ctx context.Context, pool *pgxpool.Pool, inst *models.VpsInstance) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO vps_instances (id, user_id, subscription_id, provider, provider_region, name, region, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, inst.ID, inst.UserID, inst.SubscriptionID, inst.Provider,
		inst.ProviderRegion, inst.Name, inst.Region, inst.Status)
	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}
	return nil
}

// UpdateInstanceStatus transitions an instance to a new status.
func UpdateInstanceStatus(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, status models.VpsStatus) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET status = $1, updated_at = now() WHERE id = $2
	`, status, instanceID)
	if err != nil {
		return fmt.Errorf("update instance status: %w", err)
	}
	return nil
}

// UpdateInstanceProviderInfo updates provider-specific fields after server creation.
func UpdateInstanceProviderInfo(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, providerServerID string, ipv4 *string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET provider_server_id = $1, ipv4 = $2::inet, updated_at = now() WHERE id = $3
	`, providerServerID, ipv4, instanceID)
	if err != nil {
		return fmt.Errorf("update instance provider info: %w", err)
	}
	return nil
}

// UpdateInstanceHeartbeat records the latest heartbeat.
func UpdateInstanceHeartbeat(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET last_heartbeat_at = now(), updated_at = now() WHERE id = $1
	`, instanceID)
	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}
	return nil
}

// CountActiveInstancesByUserID returns the number of non-terminated instances.
func CountActiveInstancesByUserID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM vps_instances
		WHERE user_id = $1 AND status NOT IN ('terminated', 'error')
	`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active instances: %w", err)
	}
	return count, nil
}

// CreateProvisioningJob inserts a new provisioning job.
func CreateProvisioningJob(ctx context.Context, pool *pgxpool.Pool, job *models.ProvisioningJob) error {
	maxAttempts := job.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 5
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO provisioning_jobs (id, vps_instance_id, idempotency_key, status, step, max_attempts)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, job.ID, job.VpsInstanceID, job.IdempotencyKey, job.Status, job.Step, maxAttempts)
	if err != nil {
		return fmt.Errorf("create provisioning job: %w", err)
	}
	return nil
}

// ClaimNextJob atomically claims the next pending job for processing.
func ClaimNextJob(ctx context.Context, pool *pgxpool.Pool) (*models.ProvisioningJob, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	job := &models.ProvisioningJob{}
	err = tx.QueryRow(ctx, `
		SELECT id, vps_instance_id, idempotency_key, status, step,
		       attempts, max_attempts, next_retry_at, error_message,
		       started_at, completed_at, created_at, updated_at
		FROM provisioning_jobs
		WHERE status IN ('pending', 'failed')
		  AND (next_retry_at IS NULL OR next_retry_at <= now())
		  AND attempts < max_attempts
		ORDER BY created_at
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(
		&job.ID, &job.VpsInstanceID, &job.IdempotencyKey, &job.Status, &job.Step,
		&job.Attempts, &job.MaxAttempts, &job.NextRetryAt, &job.ErrorMessage,
		&job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}

	now := time.Now()
	_, err = tx.Exec(ctx, `
		UPDATE provisioning_jobs SET status = 'running', started_at = $1, attempts = attempts + 1, updated_at = now()
		WHERE id = $2
	`, now, job.ID)
	if err != nil {
		return nil, fmt.Errorf("update claimed job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}

	job.Status = models.JobRunning
	job.StartedAt = &now
	job.Attempts++
	return job, nil
}

// UpdateJobStatus updates a job's status and optional step/error.
func UpdateJobStatus(ctx context.Context, pool *pgxpool.Pool, jobID uuid.UUID, status models.JobStatus, step *models.ProvisioningStep, errMsg *string) error {
	var completedAt *time.Time
	if status == models.JobCompleted || status == models.JobDead {
		now := time.Now()
		completedAt = &now
	}

	_, err := pool.Exec(ctx, `
		UPDATE provisioning_jobs
		SET status = $1, step = $2, error_message = $3, completed_at = $4, updated_at = now()
		WHERE id = $5
	`, status, step, errMsg, completedAt, jobID)
	if err != nil {
		return fmt.Errorf("update job status: %w", err)
	}
	return nil
}

// UpdateJobRetry sets a job to failed with a retry time.
func UpdateJobRetry(ctx context.Context, pool *pgxpool.Pool, jobID uuid.UUID, nextRetry time.Time, errMsg string) error {
	_, err := pool.Exec(ctx, `
		UPDATE provisioning_jobs
		SET status = 'failed', error_message = $1, next_retry_at = $2, updated_at = now()
		WHERE id = $3
	`, errMsg, nextRetry, jobID)
	if err != nil {
		return fmt.Errorf("update job retry: %w", err)
	}
	return nil
}

// CountPendingJobsByUserID returns the count of non-completed jobs for a user's instances.
func CountPendingJobsByUserID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM provisioning_jobs j
		JOIN vps_instances i ON j.vps_instance_id = i.id
		WHERE i.user_id = $1 AND j.status IN ('pending', 'running', 'failed')
	`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending jobs: %w", err)
	}
	return count, nil
}

// GetBestProviderMapping returns the cheapest available provider for a plan/region.
func GetBestProviderMapping(ctx context.Context, pool *pgxpool.Pool, planTier models.PlanTier, region string) (*models.ProviderPlanMapping, error) {
	m := &models.ProviderPlanMapping{}
	err := pool.QueryRow(ctx, `
		SELECT id, plan_tier, provider, region, provider_server_type,
		       provider_region, provider_image, monthly_cost_cents, is_available, created_at
		FROM provider_plan_mappings
		WHERE plan_tier = $1 AND region = $2 AND is_available = true
		ORDER BY monthly_cost_cents ASC
		LIMIT 1
	`, planTier, region).Scan(
		&m.ID, &m.PlanTier, &m.Provider, &m.Region, &m.ProviderServerType,
		&m.ProviderRegion, &m.ProviderImage, &m.MonthlyCostCents, &m.IsAvailable, &m.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get provider mapping: %w", err)
	}
	return m, nil
}

// InsertAuditLog records an audit event.
func InsertAuditLog(ctx context.Context, pool *pgxpool.Pool, entry *models.AuditLogEntry) error {
	var metadataJSON []byte
	if entry.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("marshal audit metadata: %w", err)
		}
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO audit_log (id, user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, entry.ID, entry.UserID, entry.Action, entry.ResourceType, entry.ResourceID, metadataJSON)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// GetAgentConfigByInstanceID returns the agent config for an instance.
func GetAgentConfigByInstanceID(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) (*models.AgentConfig, error) {
	ac := &models.AgentConfig{}
	var configJSON []byte
	err := pool.QueryRow(ctx, `
		SELECT id, vps_instance_id, config, version, created_at, updated_at
		FROM agent_configs WHERE vps_instance_id = $1
	`, instanceID).Scan(
		&ac.ID, &ac.VpsInstanceID, &configJSON, &ac.Version, &ac.CreatedAt, &ac.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent config: %w", err)
	}
	if err := json.Unmarshal(configJSON, &ac.Config); err != nil {
		return nil, fmt.Errorf("unmarshal agent config: %w", err)
	}
	return ac, nil
}

// UpdateInstanceRootPassword stores the root password for an instance.
func UpdateInstanceRootPassword(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, password string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET root_password = $1, updated_at = now() WHERE id = $2
	`, password, instanceID)
	if err != nil {
		return fmt.Errorf("update root password: %w", err)
	}
	return nil
}

// UpdateInstanceName sets the display name for an instance.
func UpdateInstanceName(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, name string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET name = $1, updated_at = now() WHERE id = $2
	`, name, instanceID)
	if err != nil {
		return fmt.Errorf("update instance name: %w", err)
	}
	return nil
}

// UpdateInstanceAgentToken sets the agent token for an instance.
func UpdateInstanceAgentToken(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, token string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET agent_token_secret_name = $1, updated_at = now() WHERE id = $2
	`, token, instanceID)
	if err != nil {
		return fmt.Errorf("update agent token: %w", err)
	}
	return nil
}

// GetActiveInstancesByStatus returns all instances with the given status.
func GetActiveInstancesByStatus(ctx context.Context, pool *pgxpool.Pool, status models.VpsStatus) ([]models.VpsInstance, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, subscription_id, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, last_heartbeat_at, created_at, updated_at
		FROM vps_instances
		WHERE status = $1
	`, status)
	if err != nil {
		return nil, fmt.Errorf("get instances by status: %w", err)
	}
	defer rows.Close()

	var instances []models.VpsInstance
	for rows.Next() {
		var inst models.VpsInstance
		if err := rows.Scan(
			&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Provider,
			&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
			&inst.Region, &inst.Status,
			&inst.RootPassword, &inst.AgentTokenSecretName, &inst.LastHeartbeatAt,
			&inst.CreatedAt, &inst.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

// GetSubscriptionByID returns a subscription by its ID.
func GetSubscriptionByID(ctx context.Context, pool *pgxpool.Pool, subID uuid.UUID) (*models.Subscription, error) {
	s := &models.Subscription{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, stripe_subscription_id, stripe_customer_id,
		       plan_tier, status, current_period_end, created_at, updated_at
		FROM subscriptions WHERE id = $1
	`, subID).Scan(
		&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
		&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription by id: %w", err)
	}
	return s, nil
}

// GetPastDueSubscriptions returns subscriptions that have been past_due for longer than the given duration.
func GetPastDueSubscriptions(ctx context.Context, pool *pgxpool.Pool, olderThan time.Duration) ([]models.Subscription, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, stripe_subscription_id, stripe_customer_id,
		       plan_tier, status, current_period_end, created_at, updated_at
		FROM subscriptions
		WHERE status = 'past_due' AND updated_at < $1
	`, time.Now().Add(-olderThan))
	if err != nil {
		return nil, fmt.Errorf("get past due subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var s models.Subscription
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
			&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// GetSuspendedSubscriptions returns subscriptions that have been suspended for longer than the given duration.
func GetSuspendedSubscriptions(ctx context.Context, pool *pgxpool.Pool, olderThan time.Duration) ([]models.Subscription, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, stripe_subscription_id, stripe_customer_id,
		       plan_tier, status, current_period_end, created_at, updated_at
		FROM subscriptions
		WHERE status = 'suspended' AND updated_at < $1
	`, time.Now().Add(-olderThan))
	if err != nil {
		return nil, fmt.Errorf("get suspended subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var s models.Subscription
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
			&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// GetInstancesBySubscriptionID returns all non-terminated instances for a subscription.
func GetInstancesBySubscriptionID(ctx context.Context, pool *pgxpool.Pool, subID uuid.UUID) ([]models.VpsInstance, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, subscription_id, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, last_heartbeat_at, created_at, updated_at
		FROM vps_instances
		WHERE subscription_id = $1 AND status NOT IN ('terminated', 'terminating')
	`, subID)
	if err != nil {
		return nil, fmt.Errorf("get instances by subscription: %w", err)
	}
	defer rows.Close()

	var instances []models.VpsInstance
	for rows.Next() {
		var inst models.VpsInstance
		if err := rows.Scan(
			&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Provider,
			&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
			&inst.Region, &inst.Status,
			&inst.RootPassword, &inst.AgentTokenSecretName, &inst.LastHeartbeatAt,
			&inst.CreatedAt, &inst.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}

// CreateAgentConfig inserts or updates an agent config for an instance.
func CreateAgentConfig(ctx context.Context, pool *pgxpool.Pool, ac *models.AgentConfig) error {
	configJSON, err := json.Marshal(ac.Config)
	if err != nil {
		return fmt.Errorf("marshal agent config: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO agent_configs (id, vps_instance_id, config, version)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (vps_instance_id) DO UPDATE SET
			config = EXCLUDED.config,
			version = agent_configs.version + 1,
			updated_at = now()
	`, ac.ID, ac.VpsInstanceID, configJSON, ac.Version)
	if err != nil {
		return fmt.Errorf("create agent config: %w", err)
	}
	return nil
}

// GetInstanceByAgentToken returns the instance associated with a token secret name.
func GetInstanceByAgentToken(ctx context.Context, pool *pgxpool.Pool, tokenSecretName string) (*models.VpsInstance, error) {
	inst := &models.VpsInstance{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, subscription_id, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, last_heartbeat_at, created_at, updated_at
		FROM vps_instances
		WHERE agent_token_secret_name = $1
	`, tokenSecretName).Scan(
		&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Provider,
		&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
		&inst.Region, &inst.Status,
		&inst.RootPassword, &inst.AgentTokenSecretName, &inst.LastHeartbeatAt,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get instance by agent token: %w", err)
	}
	return inst, nil
}
