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

// GetUserByID returns a user by their internal ID.
func GetUserByID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*models.User, error) {
	u := &models.User{}
	err := pool.QueryRow(ctx, `
		SELECT id, firebase_uid, email, name, created_at, updated_at
		FROM users WHERE id = $1
	`, userID).Scan(
		&u.ID, &u.FirebaseUID, &u.Email, &u.Name, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
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
		       plan_tier, status, current_period_end, cancel_at_period_end, created_at, updated_at
		FROM subscriptions WHERE user_id = $1
		ORDER BY created_at DESC LIMIT 1
	`, userID).Scan(
		&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
		&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
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
func UpdateSubscriptionStatus(ctx context.Context, pool *pgxpool.Pool, stripeSubID string, status models.SubscriptionStatus, periodEnd *time.Time, cancelAtPeriodEnd bool) error {
	_, err := pool.Exec(ctx, `
		UPDATE subscriptions SET status = $1, current_period_end = $2, cancel_at_period_end = $3, updated_at = now()
		WHERE stripe_subscription_id = $4
	`, status, periodEnd, cancelAtPeriodEnd, stripeSubID)
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
		       plan_tier, status, current_period_end, cancel_at_period_end, created_at, updated_at
		FROM subscriptions WHERE stripe_subscription_id = $1
	`, stripeSubID).Scan(
		&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
		&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
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
		SELECT id, user_id, subscription_id, framework, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       (SELECT step FROM provisioning_jobs WHERE vps_instance_id = v.id AND status IN ('pending','running','failed') ORDER BY updated_at DESC LIMIT 1),
		       root_password, agent_token_secret_name, openclaw_auth_token, agent_status, agent_error, last_heartbeat_at,
		       openclaw_version, target_openclaw_version, openclaw_update_status, openclaw_update_error,
		       domain, dns_record_id, preview_domain, preview_dns_record_id, custom_caddyfile,
		       created_at, updated_at
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
			&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Framework, &inst.Provider,
			&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
			&inst.Region, &inst.Status, &inst.Step,
			&inst.RootPassword, &inst.AgentTokenSecretName, &inst.OpenClawAuthToken, &inst.AgentStatus, &inst.AgentError, &inst.LastHeartbeatAt,
			&inst.OpenClawVersion, &inst.TargetOpenClawVersion, &inst.OpenClawUpdateStatus, &inst.OpenClawUpdateError,
			&inst.Domain, &inst.DNSRecordID, &inst.PreviewDomain, &inst.PreviewDNSRecordID, &inst.CustomCaddyfile,
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
		SELECT id, user_id, subscription_id, framework, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       (SELECT step FROM provisioning_jobs WHERE vps_instance_id = v.id AND status IN ('pending','running','failed') ORDER BY updated_at DESC LIMIT 1),
		       root_password, agent_token_secret_name, openclaw_auth_token, agent_status, agent_error, last_heartbeat_at,
		       openclaw_version, target_openclaw_version, openclaw_update_status, openclaw_update_error,
		       domain, dns_record_id, preview_domain, preview_dns_record_id, custom_caddyfile,
		       created_at, updated_at
		FROM vps_instances v
		WHERE id = $1 AND user_id = $2
	`, instanceID, userID).Scan(
		&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Framework, &inst.Provider,
		&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
		&inst.Region, &inst.Status, &inst.Step,
		&inst.RootPassword, &inst.AgentTokenSecretName, &inst.OpenClawAuthToken, &inst.AgentStatus, &inst.AgentError, &inst.LastHeartbeatAt,
		&inst.OpenClawVersion, &inst.TargetOpenClawVersion, &inst.OpenClawUpdateStatus, &inst.OpenClawUpdateError,
		&inst.Domain, &inst.DNSRecordID, &inst.PreviewDomain, &inst.PreviewDNSRecordID, &inst.CustomCaddyfile,
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
		INSERT INTO vps_instances (id, user_id, subscription_id, framework, provider, provider_region, name, region, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, inst.ID, inst.UserID, inst.SubscriptionID, inst.Framework, inst.Provider,
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

// UpdateInstanceHeartbeat records the latest heartbeat and optional agent status.
func UpdateInstanceHeartbeat(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, agentStatus *string, agentError *string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET last_heartbeat_at = now(), agent_status = COALESCE($2, agent_status), agent_error = $3, updated_at = now() WHERE id = $1
	`, instanceID, agentStatus, agentError)
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

// UpdateInstanceRootPasswordByIP updates the root password for an instance identified by IP.
func UpdateInstanceRootPasswordByIP(ctx context.Context, pool *pgxpool.Pool, ip string, password string) error {
	tag, err := pool.Exec(ctx, `
		UPDATE vps_instances SET root_password = $1, updated_at = now() WHERE host(ipv4)::text = $2
	`, password, ip)
	if err != nil {
		return fmt.Errorf("update root password by ip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no instance found with ip %s", ip)
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

// UpdateInstanceOpenClawAuthToken sets the OpenClaw auth token for an instance.
func UpdateInstanceOpenClawAuthToken(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, token string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET openclaw_auth_token = $1, updated_at = now() WHERE id = $2
	`, token, instanceID)
	if err != nil {
		return fmt.Errorf("update openclaw auth token: %w", err)
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
		SELECT id, user_id, subscription_id, framework, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, openclaw_auth_token, agent_status, agent_error, last_heartbeat_at,
		       openclaw_version, target_openclaw_version, openclaw_update_status, openclaw_update_error,
		       domain, dns_record_id, preview_domain, preview_dns_record_id, custom_caddyfile,
		       created_at, updated_at
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
			&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Framework, &inst.Provider,
			&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
			&inst.Region, &inst.Status,
			&inst.RootPassword, &inst.AgentTokenSecretName, &inst.OpenClawAuthToken, &inst.AgentStatus, &inst.AgentError, &inst.LastHeartbeatAt,
			&inst.OpenClawVersion, &inst.TargetOpenClawVersion, &inst.OpenClawUpdateStatus, &inst.OpenClawUpdateError,
			&inst.Domain, &inst.DNSRecordID, &inst.PreviewDomain, &inst.PreviewDNSRecordID, &inst.CustomCaddyfile,
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
		       plan_tier, status, current_period_end, cancel_at_period_end, created_at, updated_at
		FROM subscriptions WHERE id = $1
	`, subID).Scan(
		&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
		&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
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
		       plan_tier, status, current_period_end, cancel_at_period_end, created_at, updated_at
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
			&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
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
		       plan_tier, status, current_period_end, cancel_at_period_end, created_at, updated_at
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
			&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// GetCanceledSubscriptions returns subscriptions with status 'canceled' that still have resources to clean up.
func GetCanceledSubscriptions(ctx context.Context, pool *pgxpool.Pool) ([]models.Subscription, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, stripe_subscription_id, stripe_customer_id,
		       plan_tier, status, current_period_end, cancel_at_period_end, created_at, updated_at
		FROM subscriptions
		WHERE status = 'canceled'
	`)
	if err != nil {
		return nil, fmt.Errorf("get canceled subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var s models.Subscription
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.StripeSubscriptionID, &s.StripeCustomerID,
			&s.PlanTier, &s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// GetSnapshotsByInstanceID returns all non-deleted snapshots for an instance.
func GetSnapshotsByInstanceID(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) ([]models.Snapshot, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, vps_instance_id, provider_image_id, name, status,
		       size_gb, error_message, is_system, created_at, updated_at
		FROM snapshots
		WHERE vps_instance_id = $1 AND status != 'deleted' AND is_system = false
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get snapshots by instance: %w", err)
	}
	defer rows.Close()

	var snapshots []models.Snapshot
	for rows.Next() {
		var s models.Snapshot
		if err := rows.Scan(
			&s.ID, &s.VpsInstanceID, &s.ProviderImageID, &s.Name, &s.Status,
			&s.SizeGB, &s.ErrorMessage, &s.IsSystem, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

// GetInstancesBySubscriptionID returns all non-terminated instances for a subscription.
func GetInstancesBySubscriptionID(ctx context.Context, pool *pgxpool.Pool, subID uuid.UUID) ([]models.VpsInstance, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, subscription_id, framework, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, openclaw_auth_token, agent_status, agent_error, last_heartbeat_at,
		       openclaw_version, target_openclaw_version, openclaw_update_status, openclaw_update_error,
		       domain, dns_record_id, preview_domain, preview_dns_record_id, custom_caddyfile,
		       created_at, updated_at
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
			&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Framework, &inst.Provider,
			&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
			&inst.Region, &inst.Status,
			&inst.RootPassword, &inst.AgentTokenSecretName, &inst.OpenClawAuthToken, &inst.AgentStatus, &inst.AgentError, &inst.LastHeartbeatAt,
			&inst.OpenClawVersion, &inst.TargetOpenClawVersion, &inst.OpenClawUpdateStatus, &inst.OpenClawUpdateError,
			&inst.Domain, &inst.DNSRecordID, &inst.PreviewDomain, &inst.PreviewDNSRecordID, &inst.CustomCaddyfile,
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

// CreateSnapshot inserts a new snapshot record.
func CreateSnapshot(ctx context.Context, pool *pgxpool.Pool, snap *models.Snapshot) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshots (id, vps_instance_id, name, status, is_system)
		VALUES ($1, $2, $3, $4, $5)
	`, snap.ID, snap.VpsInstanceID, snap.Name, snap.Status, snap.IsSystem)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	return nil
}

// GetSnapshotByID returns a snapshot by ID, scoped to a user via JOIN.
func GetSnapshotByID(ctx context.Context, pool *pgxpool.Pool, snapshotID uuid.UUID, userID uuid.UUID) (*models.Snapshot, error) {
	s := &models.Snapshot{}
	err := pool.QueryRow(ctx, `
		SELECT s.id, s.vps_instance_id, s.provider_image_id, s.name, s.status,
		       s.size_gb, s.error_message, s.is_system, s.created_at, s.updated_at
		FROM snapshots s
		JOIN vps_instances i ON s.vps_instance_id = i.id
		WHERE s.id = $1 AND i.user_id = $2
	`, snapshotID, userID).Scan(
		&s.ID, &s.VpsInstanceID, &s.ProviderImageID, &s.Name, &s.Status,
		&s.SizeGB, &s.ErrorMessage, &s.IsSystem, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get snapshot by id: %w", err)
	}
	return s, nil
}

// GetSnapshotsByUserID returns all non-deleted snapshots for a user.
func GetSnapshotsByUserID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]models.Snapshot, error) {
	rows, err := pool.Query(ctx, `
		SELECT s.id, s.vps_instance_id, s.provider_image_id, s.name, s.status,
		       s.size_gb, s.error_message, s.is_system, s.created_at, s.updated_at
		FROM snapshots s
		JOIN vps_instances i ON s.vps_instance_id = i.id
		WHERE i.user_id = $1 AND s.status != 'deleted' AND s.is_system = false
		ORDER BY s.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get snapshots by user: %w", err)
	}
	defer rows.Close()

	var snapshots []models.Snapshot
	for rows.Next() {
		var s models.Snapshot
		if err := rows.Scan(
			&s.ID, &s.VpsInstanceID, &s.ProviderImageID, &s.Name, &s.Status,
			&s.SizeGB, &s.ErrorMessage, &s.IsSystem, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

// CountActiveSnapshotsByInstanceID returns the count of non-deleted/non-error snapshots.
func CountActiveSnapshotsByInstanceID(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM snapshots
		WHERE vps_instance_id = $1 AND status NOT IN ('deleted', 'error', 'deleting') AND is_system = false
	`, instanceID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active snapshots: %w", err)
	}
	return count, nil
}

// UpdateSnapshotReady marks a snapshot as ready with provider info.
func UpdateSnapshotReady(ctx context.Context, pool *pgxpool.Pool, snapshotID uuid.UUID, providerImageID string, sizeGB float32) error {
	_, err := pool.Exec(ctx, `
		UPDATE snapshots SET status = 'ready', provider_image_id = $1, size_gb = $2, updated_at = now()
		WHERE id = $3
	`, providerImageID, sizeGB, snapshotID)
	if err != nil {
		return fmt.Errorf("update snapshot ready: %w", err)
	}
	return nil
}

// UpdateSnapshotStatus sets the status of a snapshot.
func UpdateSnapshotStatus(ctx context.Context, pool *pgxpool.Pool, snapshotID uuid.UUID, status models.SnapshotStatus) error {
	_, err := pool.Exec(ctx, `
		UPDATE snapshots SET status = $1, updated_at = now() WHERE id = $2
	`, status, snapshotID)
	if err != nil {
		return fmt.Errorf("update snapshot status: %w", err)
	}
	return nil
}

// UpdateSnapshotError marks a snapshot as error with a message.
func UpdateSnapshotError(ctx context.Context, pool *pgxpool.Pool, snapshotID uuid.UUID, errMsg string) error {
	_, err := pool.Exec(ctx, `
		UPDATE snapshots SET status = 'error', error_message = $1, updated_at = now() WHERE id = $2
	`, errMsg, snapshotID)
	if err != nil {
		return fmt.Errorf("update snapshot error: %w", err)
	}
	return nil
}

// GetSystemSnapshotByInstanceID returns the latest ready system snapshot for an instance.
func GetSystemSnapshotByInstanceID(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) (*models.Snapshot, error) {
	s := &models.Snapshot{}
	err := pool.QueryRow(ctx, `
		SELECT id, vps_instance_id, provider_image_id, name, status,
		       size_gb, error_message, is_system, created_at, updated_at
		FROM snapshots
		WHERE vps_instance_id = $1 AND is_system = true AND status = 'ready'
		ORDER BY created_at DESC LIMIT 1
	`, instanceID).Scan(
		&s.ID, &s.VpsInstanceID, &s.ProviderImageID, &s.Name, &s.Status,
		&s.SizeGB, &s.ErrorMessage, &s.IsSystem, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get system snapshot by instance: %w", err)
	}
	return s, nil
}

// ClearInstanceProviderInfo nulls out the provider server ID and IP for an instance.
func ClearInstanceProviderInfo(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET provider_server_id = NULL, ipv4 = NULL, updated_at = now()
		WHERE id = $1
	`, instanceID)
	if err != nil {
		return fmt.Errorf("clear instance provider info: %w", err)
	}
	return nil
}

// GetAllSnapshotsByInstanceID returns all non-deleted snapshots (including system) for an instance.
func GetAllSnapshotsByInstanceID(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) ([]models.Snapshot, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, vps_instance_id, provider_image_id, name, status,
		       size_gb, error_message, is_system, created_at, updated_at
		FROM snapshots
		WHERE vps_instance_id = $1 AND status != 'deleted'
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get all snapshots by instance: %w", err)
	}
	defer rows.Close()

	var snapshots []models.Snapshot
	for rows.Next() {
		var s models.Snapshot
		if err := rows.Scan(
			&s.ID, &s.VpsInstanceID, &s.ProviderImageID, &s.Name, &s.Status,
			&s.SizeGB, &s.ErrorMessage, &s.IsSystem, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

// GetInstanceByAgentToken returns the instance associated with a token secret name.
func GetInstanceByAgentToken(ctx context.Context, pool *pgxpool.Pool, tokenSecretName string) (*models.VpsInstance, error) {
	inst := &models.VpsInstance{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, subscription_id, framework, provider, provider_server_id, provider_region,
		       name, host(ipv4)::text, region, status,
		       root_password, agent_token_secret_name, openclaw_auth_token, agent_status, agent_error, last_heartbeat_at,
		       openclaw_version, target_openclaw_version, openclaw_update_status, openclaw_update_error,
		       domain, dns_record_id, preview_domain, preview_dns_record_id, custom_caddyfile,
		       created_at, updated_at
		FROM vps_instances
		WHERE agent_token_secret_name = $1
	`, tokenSecretName).Scan(
		&inst.ID, &inst.UserID, &inst.SubscriptionID, &inst.Framework, &inst.Provider,
		&inst.ProviderServerID, &inst.ProviderRegion, &inst.Name, &inst.IPv4,
		&inst.Region, &inst.Status,
		&inst.RootPassword, &inst.AgentTokenSecretName, &inst.OpenClawAuthToken, &inst.AgentStatus, &inst.AgentError, &inst.LastHeartbeatAt,
		&inst.OpenClawVersion, &inst.TargetOpenClawVersion, &inst.OpenClawUpdateStatus, &inst.OpenClawUpdateError,
		&inst.Domain, &inst.DNSRecordID, &inst.PreviewDomain, &inst.PreviewDNSRecordID, &inst.CustomCaddyfile,
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

// GetPlatformSetting returns the value for a platform_settings key, or ErrNotFound.
func GetPlatformSetting(ctx context.Context, pool *pgxpool.Pool, key string) (string, error) {
	var value string
	err := pool.QueryRow(ctx, `
		SELECT value FROM platform_settings WHERE key = $1
	`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get platform setting %q: %w", key, err)
	}
	return value, nil
}

// UpsertPlatformSetting inserts or updates a platform_settings key.
func UpsertPlatformSetting(ctx context.Context, pool *pgxpool.Pool, key, value string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO platform_settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()
	`, key, value)
	if err != nil {
		return fmt.Errorf("upsert platform setting %q: %w", key, err)
	}
	return nil
}

// GetGlobalTargetVersion returns the platform-wide target OpenClaw version.
func GetGlobalTargetVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	value, err := GetPlatformSetting(ctx, pool, "target_openclaw_version")
	if errors.Is(err, ErrNotFound) {
		return "latest", nil
	}
	return value, err
}

// SetGlobalTargetVersion updates the platform-wide target OpenClaw version.
func SetGlobalTargetVersion(ctx context.Context, pool *pgxpool.Pool, version string) error {
	return UpsertPlatformSetting(ctx, pool, "target_openclaw_version", version)
}

// GetGlobalHermesVersion returns the platform-wide target Hermes version.
func GetGlobalHermesVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	value, err := GetPlatformSetting(ctx, pool, "target_hermes_version")
	if errors.Is(err, ErrNotFound) {
		return "latest", nil
	}
	return value, err
}

// SetGlobalHermesVersion updates the platform-wide target Hermes version.
func SetGlobalHermesVersion(ctx context.Context, pool *pgxpool.Pool, version string) error {
	return UpsertPlatformSetting(ctx, pool, "target_hermes_version", version)
}

// UpdateInstanceOpenClawVersion records the version the agent reports and its update status.
func UpdateInstanceOpenClawVersion(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, version string, updateStatus *string, updateError *string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances
		SET openclaw_version = $1, openclaw_update_status = $2, openclaw_update_error = $3, updated_at = now()
		WHERE id = $4
	`, version, updateStatus, updateError, instanceID)
	if err != nil {
		return fmt.Errorf("update openclaw version: %w", err)
	}
	return nil
}

// UpdateInstanceDomain stores the domain and Cloudflare DNS record ID for an instance.
func UpdateInstanceDomain(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, domain, dnsRecordID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET domain = $1, dns_record_id = $2, updated_at = now() WHERE id = $3
	`, domain, dnsRecordID, instanceID)
	if err != nil {
		return fmt.Errorf("update instance domain: %w", err)
	}
	return nil
}

// UpdateInstancePreviewDNS stores the preview domain and Cloudflare DNS record ID for an instance.
func UpdateInstancePreviewDNS(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, domain, dnsRecordID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET preview_domain = $1, preview_dns_record_id = $2, updated_at = now() WHERE id = $3
	`, domain, dnsRecordID, instanceID)
	if err != nil {
		return fmt.Errorf("update instance preview DNS: %w", err)
	}
	return nil
}

// UpdateSubscriptionPlanTier changes the plan tier for a subscription.
func UpdateSubscriptionPlanTier(ctx context.Context, pool *pgxpool.Pool, subID uuid.UUID, planTier models.PlanTier) error {
	_, err := pool.Exec(ctx, `
		UPDATE subscriptions SET plan_tier = $1, updated_at = now() WHERE id = $2
	`, planTier, subID)
	if err != nil {
		return fmt.Errorf("update subscription plan tier: %w", err)
	}
	return nil
}

// SetInstanceTargetVersion sets a per-instance target version override (nil to clear).
func SetInstanceTargetVersion(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, version *string) error {
	_, err := pool.Exec(ctx, `
		UPDATE vps_instances SET target_openclaw_version = $1, updated_at = now() WHERE id = $2
	`, version, instanceID)
	if err != nil {
		return fmt.Errorf("set instance target version: %w", err)
	}
	return nil
}

// ListEnabledModels returns all enabled models ordered by sort_order.
func ListEnabledModels(ctx context.Context, pool *pgxpool.Pool) ([]models.Model, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, display_name, provider, tier, is_enabled, is_default, sort_order, tags, created_at, updated_at
		FROM models
		WHERE is_enabled = true
		ORDER BY sort_order ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list enabled models: %w", err)
	}
	defer rows.Close()

	var result []models.Model
	for rows.Next() {
		var m models.Model
		if err := rows.Scan(&m.ID, &m.DisplayName, &m.Provider, &m.Tier, &m.IsEnabled, &m.IsDefault, &m.SortOrder, &m.Tags, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("list enabled models scan: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// GetDefaultModel returns the default model. Falls back to the first enabled free model.
func GetDefaultModel(ctx context.Context, pool *pgxpool.Pool) (*models.Model, error) {
	m := &models.Model{}
	err := pool.QueryRow(ctx, `
		SELECT id, display_name, provider, tier, is_enabled, is_default, sort_order, tags, created_at, updated_at
		FROM models
		WHERE is_enabled = true AND is_default = true
		LIMIT 1
	`).Scan(&m.ID, &m.DisplayName, &m.Provider, &m.Tier, &m.IsEnabled, &m.IsDefault, &m.SortOrder, &m.Tags, &m.CreatedAt, &m.UpdatedAt)
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get default model: %w", err)
	}

	// Fallback: first enabled free model
	err = pool.QueryRow(ctx, `
		SELECT id, display_name, provider, tier, is_enabled, is_default, sort_order, tags, created_at, updated_at
		FROM models
		WHERE is_enabled = true AND tier = 'free'
		ORDER BY sort_order ASC
		LIMIT 1
	`).Scan(&m.ID, &m.DisplayName, &m.Provider, &m.Tier, &m.IsEnabled, &m.IsDefault, &m.SortOrder, &m.Tags, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get default model fallback: %w", err)
	}
	return m, nil
}

// IsModelValid checks if a model ID exists and is enabled.
func IsModelValid(ctx context.Context, pool *pgxpool.Pool, modelID string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM models WHERE id = $1 AND is_enabled = true)
	`, modelID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("is model valid: %w", err)
	}
	return exists, nil
}

// UpsertGoogleOAuthToken inserts or updates the Google OAuth token for a user.
func UpsertGoogleOAuthToken(ctx context.Context, pool *pgxpool.Pool, token *models.GoogleOAuthToken) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO google_oauth_tokens (id, user_id, google_email, access_token_enc, refresh_token_enc, token_expiry, scopes, revoked)
		VALUES ($1, $2, $3, $4, $5, $6, $7, false)
		ON CONFLICT (user_id) DO UPDATE SET
			google_email = EXCLUDED.google_email,
			access_token_enc = EXCLUDED.access_token_enc,
			refresh_token_enc = EXCLUDED.refresh_token_enc,
			token_expiry = EXCLUDED.token_expiry,
			scopes = EXCLUDED.scopes,
			revoked = false,
			updated_at = now()
	`, token.ID, token.UserID, token.GoogleEmail, token.AccessTokenEnc, token.RefreshTokenEnc, token.TokenExpiry, token.Scopes)
	if err != nil {
		return fmt.Errorf("upsert google oauth token: %w", err)
	}
	return nil
}

// GetGoogleOAuthTokenByUserID returns the non-revoked Google OAuth token for a user.
func GetGoogleOAuthTokenByUserID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*models.GoogleOAuthToken, error) {
	t := &models.GoogleOAuthToken{}
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, google_email, access_token_enc, refresh_token_enc, token_expiry, scopes, revoked, created_at, updated_at
		FROM google_oauth_tokens
		WHERE user_id = $1 AND revoked = false
	`, userID).Scan(
		&t.ID, &t.UserID, &t.GoogleEmail, &t.AccessTokenEnc, &t.RefreshTokenEnc,
		&t.TokenExpiry, &t.Scopes, &t.Revoked, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get google oauth token by user id: %w", err)
	}
	return t, nil
}

// GetGoogleOAuthTokenByInstanceID returns the Google OAuth token for the user who owns an instance.
func GetGoogleOAuthTokenByInstanceID(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID) (*models.GoogleOAuthToken, error) {
	t := &models.GoogleOAuthToken{}
	err := pool.QueryRow(ctx, `
		SELECT g.id, g.user_id, g.google_email, g.access_token_enc, g.refresh_token_enc, g.token_expiry, g.scopes, g.revoked, g.created_at, g.updated_at
		FROM google_oauth_tokens g
		JOIN vps_instances i ON i.user_id = g.user_id
		WHERE i.id = $1 AND g.revoked = false
	`, instanceID).Scan(
		&t.ID, &t.UserID, &t.GoogleEmail, &t.AccessTokenEnc, &t.RefreshTokenEnc,
		&t.TokenExpiry, &t.Scopes, &t.Revoked, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get google oauth token by instance id: %w", err)
	}
	return t, nil
}

// RevokeGoogleOAuthToken marks the Google OAuth token as revoked.
func RevokeGoogleOAuthToken(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		UPDATE google_oauth_tokens SET revoked = true, updated_at = now() WHERE user_id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("revoke google oauth token: %w", err)
	}
	return nil
}

// GetExpiringSoonGoogleTokens returns non-revoked tokens expiring within the given threshold.
func GetExpiringSoonGoogleTokens(ctx context.Context, pool *pgxpool.Pool, threshold time.Duration) ([]*models.GoogleOAuthToken, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, google_email, access_token_enc, refresh_token_enc, token_expiry, scopes, revoked, created_at, updated_at
		FROM google_oauth_tokens
		WHERE revoked = false AND token_expiry < now() + $1::interval
	`, threshold)
	if err != nil {
		return nil, fmt.Errorf("get expiring google tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*models.GoogleOAuthToken
	for rows.Next() {
		t := &models.GoogleOAuthToken{}
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.GoogleEmail, &t.AccessTokenEnc, &t.RefreshTokenEnc,
			&t.TokenExpiry, &t.Scopes, &t.Revoked, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("get expiring google tokens scan: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// UpdateGoogleOAuthTokenFields updates the encrypted tokens and expiry for a Google OAuth token.
func UpdateGoogleOAuthTokenFields(ctx context.Context, pool *pgxpool.Pool, tokenID uuid.UUID, accessEnc, refreshEnc []byte, expiry time.Time) error {
	_, err := pool.Exec(ctx, `
		UPDATE google_oauth_tokens
		SET access_token_enc = $1, refresh_token_enc = $2, token_expiry = $3, updated_at = now()
		WHERE id = $4
	`, accessEnc, refreshEnc, expiry, tokenID)
	if err != nil {
		return fmt.Errorf("update google oauth token: %w", err)
	}
	return nil
}

// --- Race condition safeguards ---

// UpdateInstanceStatusConditional updates status only if the current status matches expectedStatus.
// Returns ErrConflict if no rows were updated (status already changed).
func UpdateInstanceStatusConditional(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, expectedStatus models.VpsStatus, newStatus models.VpsStatus) error {
	result, err := pool.Exec(ctx, `
		UPDATE vps_instances SET status = $1, updated_at = now()
		WHERE id = $2 AND status = $3
	`, newStatus, instanceID, expectedStatus)
	if err != nil {
		return fmt.Errorf("conditional status update: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// UpdateInstanceStatusConditionalNot updates status only if the current status is NOT in excludeStatuses.
// Returns ErrConflict if no rows were updated.
func UpdateInstanceStatusConditionalNot(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, excludeStatuses []models.VpsStatus, newStatus models.VpsStatus) error {
	// Convert to string slice for PostgreSQL array parameter
	strs := make([]string, len(excludeStatuses))
	for i, s := range excludeStatuses {
		strs[i] = string(s)
	}
	result, err := pool.Exec(ctx, `
		UPDATE vps_instances SET status = $1, updated_at = now()
		WHERE id = $2 AND status != ALL($3::text[])
	`, newStatus, instanceID, strs)
	if err != nil {
		return fmt.Errorf("conditional status update: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// CreateSnapshotWithLimit atomically checks the snapshot count and inserts a new snapshot.
// Returns ErrLimitReached if the limit would be exceeded.
func CreateSnapshotWithLimit(ctx context.Context, pool *pgxpool.Pool, snap *models.Snapshot, limit int) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the instance row to serialize snapshot creation for this instance
	var instanceID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM vps_instances WHERE id = $1 FOR UPDATE
	`, snap.VpsInstanceID).Scan(&instanceID)
	if err != nil {
		return fmt.Errorf("lock instance: %w", err)
	}

	// Count active snapshots within the transaction (after acquiring lock)
	var count int
	err = tx.QueryRow(ctx, `
		SELECT count(*) FROM snapshots
		WHERE vps_instance_id = $1 AND status NOT IN ('deleted', 'error', 'deleting') AND is_system = false
	`, snap.VpsInstanceID).Scan(&count)
	if err != nil {
		return fmt.Errorf("count snapshots: %w", err)
	}
	if count >= limit {
		return ErrLimitReached
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO snapshots (id, vps_instance_id, name, status, is_system)
		VALUES ($1, $2, $3, $4, $5)
	`, snap.ID, snap.VpsInstanceID, snap.Name, snap.Status, snap.IsSystem)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}

	return tx.Commit(ctx)
}

// UpdateAgentConfigAtomic reads existing config under a row lock, merges with updates, and writes atomically.
// preserveKeys are config keys to preserve from the existing config if not present in updates.
func UpdateAgentConfigAtomic(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, updates map[string]any, preserveKeys []string) (*models.AgentConfig, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Read existing config with lock
	var existing models.AgentConfig
	var configJSON []byte
	err = tx.QueryRow(ctx, `
		SELECT id, vps_instance_id, config, version, created_at, updated_at
		FROM agent_configs WHERE vps_instance_id = $1
		FOR UPDATE
	`, instanceID).Scan(&existing.ID, &existing.VpsInstanceID, &configJSON, &existing.Version, &existing.CreatedAt, &existing.UpdatedAt)

	hasExisting := err == nil
	if hasExisting {
		_ = json.Unmarshal(configJSON, &existing.Config)
	}

	// Merge: preserve existing keys not present in updates
	merged := updates
	if hasExisting {
		for _, key := range preserveKeys {
			if merged[key] == nil {
				if v, ok := existing.Config[key].(string); ok && v != "" {
					merged[key] = v
				}
			}
		}
	}

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	// Upsert with version increment
	var result models.AgentConfig
	var resultConfigJSON []byte
	err = tx.QueryRow(ctx, `
		INSERT INTO agent_configs (id, vps_instance_id, config, version)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (vps_instance_id) DO UPDATE SET
			config = $3,
			version = agent_configs.version + 1,
			updated_at = now()
		RETURNING id, vps_instance_id, config, version, created_at, updated_at
	`, uuid.New(), instanceID, mergedJSON).Scan(
		&result.ID, &result.VpsInstanceID, &resultConfigJSON,
		&result.Version, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert config: %w", err)
	}
	_ = json.Unmarshal(resultConfigJSON, &result.Config)

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &result, nil
}

// UpdateSubscriptionStatusReturningPrev atomically updates subscription status and returns the previous status.
// The subquery in RETURNING reads the pre-update value within the same statement.
func UpdateSubscriptionStatusReturningPrev(ctx context.Context, pool *pgxpool.Pool, stripeSubID string, status models.SubscriptionStatus, periodEnd *time.Time, cancelAtPeriodEnd bool) (prevStatus models.SubscriptionStatus, subID uuid.UUID, err error) {
	err = pool.QueryRow(ctx, `
		UPDATE subscriptions
		SET status = $1, current_period_end = $2, cancel_at_period_end = $3, updated_at = now()
		WHERE stripe_subscription_id = $4
		RETURNING (SELECT status FROM subscriptions WHERE stripe_subscription_id = $4), id
	`, status, periodEnd, cancelAtPeriodEnd, stripeSubID).Scan(&prevStatus, &subID)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("update subscription status: %w", err)
	}
	return prevStatus, subID, nil
}
