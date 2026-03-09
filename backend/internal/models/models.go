package models

import (
	"time"

	"github.com/google/uuid"
)

type VpsStatus string

const (
	VpsStatusRequested       VpsStatus = "requested"
	VpsStatusProvisioning    VpsStatus = "provisioning"
	VpsStatusBootstrapping   VpsStatus = "bootstrapping"
	VpsStatusInstallingAgent VpsStatus = "installing_agent"
	VpsStatusActive          VpsStatus = "active"
	VpsStatusRestarting      VpsStatus = "restarting"
	VpsStatusSuspending      VpsStatus = "suspending"
	VpsStatusSuspended       VpsStatus = "suspended"
	VpsStatusResuming        VpsStatus = "resuming"
	VpsStatusTerminating     VpsStatus = "terminating"
	VpsStatusTerminated      VpsStatus = "terminated"
	VpsStatusSnapshotting    VpsStatus = "snapshotting"
	VpsStatusRestoring       VpsStatus = "restoring"
	VpsStatusError           VpsStatus = "error"
)

type ProvisioningStep string

const (
	StepSelectProvider  ProvisioningStep = "select_provider"
	StepCreateServer    ProvisioningStep = "create_server"
	StepWaitServerReady ProvisioningStep = "wait_server_ready"
	StepBootstrap       ProvisioningStep = "bootstrap"
	StepInstallAgent    ProvisioningStep = "install_agent"
	StepActivate        ProvisioningStep = "activate"
)

type SubscriptionStatus string

const (
	SubStatusActive    SubscriptionStatus = "active"
	SubStatusPastDue   SubscriptionStatus = "past_due"
	SubStatusCanceled  SubscriptionStatus = "canceled"
	SubStatusSuspended SubscriptionStatus = "suspended"
)

type PlanTier string

const (
	PlanStandard PlanTier = "standard"
)

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobDead      JobStatus = "dead"
)

type User struct {
	ID          uuid.UUID
	FirebaseUID string
	Email       string
	Name        *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Subscription struct {
	ID                   uuid.UUID
	UserID               uuid.UUID
	StripeSubscriptionID string
	StripeCustomerID     string
	PlanTier             PlanTier
	Status               SubscriptionStatus
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type VpsInstance struct {
	ID                   uuid.UUID
	UserID               uuid.UUID
	SubscriptionID       uuid.UUID
	Provider             string
	ProviderServerID     *string
	ProviderRegion       *string
	Name                 string
	IPv4                 *string
	Region               string
	Status               VpsStatus
	Step                 *ProvisioningStep
	RootPassword         *string
	AgentTokenSecretName *string
	LastHeartbeatAt      *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type ProvisioningJob struct {
	ID             uuid.UUID
	VpsInstanceID  uuid.UUID
	IdempotencyKey string
	Status         JobStatus
	Step           *ProvisioningStep
	Attempts       int
	MaxAttempts    int
	NextRetryAt    *time.Time
	ErrorMessage   *string
	StartedAt      *time.Time
	CompletedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ProviderPlanMapping struct {
	ID                 uuid.UUID
	PlanTier           PlanTier
	Provider           string
	Region             string
	ProviderServerType string
	ProviderRegion     string
	ProviderImage      string
	MonthlyCostCents   int
	IsAvailable        bool
	CreatedAt          time.Time
}

type AgentConfig struct {
	ID            uuid.UUID
	VpsInstanceID uuid.UUID
	Config        map[string]any
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SnapshotStatus string

const (
	SnapshotStatusCreating SnapshotStatus = "creating"
	SnapshotStatusReady    SnapshotStatus = "ready"
	SnapshotStatusDeleting SnapshotStatus = "deleting"
	SnapshotStatusError    SnapshotStatus = "error"
	SnapshotStatusDeleted  SnapshotStatus = "deleted"
)

type Snapshot struct {
	ID              uuid.UUID
	VpsInstanceID   uuid.UUID
	ProviderImageID *string
	Name            string
	Status          SnapshotStatus
	SizeGB          *float32
	ErrorMessage    *string
	IsSystem        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type AuditLogEntry struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Metadata     map[string]any
	CreatedAt    time.Time
}
