package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func strPtr(s string) *string {
	return &s
}

func TestToInstanceResponse_Basic(t *testing.T) {
	id := uuid.New()
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)

	inst := VpsInstance{
		ID:        id,
		Name:      "my-agent",
		Status:    VpsStatusActive,
		Provider:  "hetzner",
		Region:    "us-east",
		CreatedAt: now,
	}

	r := ToInstanceResponse(inst)

	if r.ID != id.String() {
		t.Errorf("ID = %q, want %q", r.ID, id.String())
	}
	if r.Name != "my-agent" {
		t.Errorf("Name = %q, want %q", r.Name, "my-agent")
	}
	if r.Status != "active" {
		t.Errorf("Status = %q, want %q", r.Status, "active")
	}
	if r.Provider != "hetzner" {
		t.Errorf("Provider = %q, want %q", r.Provider, "hetzner")
	}
	if r.Region != "us-east" {
		t.Errorf("Region = %q, want %q", r.Region, "us-east")
	}
	if r.CreatedAt != now.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", r.CreatedAt, now.Format(time.RFC3339))
	}
}

func TestToInstanceResponse_DashboardURL_WithDomain(t *testing.T) {
	inst := VpsInstance{
		ID:        uuid.New(),
		Name:      "agent",
		Provider:  "hetzner",
		Region:    "us-east",
		Status:    VpsStatusActive,
		Domain:    strPtr("agent.tardi.ai"),
		IPv4:      strPtr("1.2.3.4"),
		CreatedAt: time.Now(),
	}

	r := ToInstanceResponse(inst)

	if r.DashboardURL == nil {
		t.Fatal("DashboardURL is nil, want non-nil")
	}
	if *r.DashboardURL != "https://agent.tardi.ai" {
		t.Errorf("DashboardURL = %q, want %q", *r.DashboardURL, "https://agent.tardi.ai")
	}
}

func TestToInstanceResponse_DashboardURL_NoDomain_WithIPv4(t *testing.T) {
	inst := VpsInstance{
		ID:        uuid.New(),
		Name:      "agent",
		Provider:  "hetzner",
		Region:    "us-east",
		Status:    VpsStatusActive,
		IPv4:      strPtr("1.2.3.4"),
		CreatedAt: time.Now(),
	}

	r := ToInstanceResponse(inst)

	if r.DashboardURL == nil {
		t.Fatal("DashboardURL is nil, want non-nil")
	}
	if *r.DashboardURL != "https://1.2.3.4" {
		t.Errorf("DashboardURL = %q, want %q", *r.DashboardURL, "https://1.2.3.4")
	}
}

func TestToInstanceResponse_DashboardURL_NoDomain_NoIPv4(t *testing.T) {
	inst := VpsInstance{
		ID:        uuid.New(),
		Name:      "agent",
		Provider:  "hetzner",
		Region:    "us-east",
		Status:    VpsStatusActive,
		CreatedAt: time.Now(),
	}

	r := ToInstanceResponse(inst)

	if r.DashboardURL != nil {
		t.Errorf("DashboardURL = %q, want nil", *r.DashboardURL)
	}
}

func TestToInstanceResponse_PreviewURL(t *testing.T) {
	inst := VpsInstance{
		ID:            uuid.New(),
		Name:          "agent",
		Provider:      "hetzner",
		Region:        "us-east",
		Status:        VpsStatusActive,
		PreviewDomain: strPtr("preview.tardi.ai"),
		CreatedAt:     time.Now(),
	}

	r := ToInstanceResponse(inst)

	if r.PreviewURL == nil {
		t.Fatal("PreviewURL is nil, want non-nil")
	}
	if *r.PreviewURL != "https://preview.tardi.ai" {
		t.Errorf("PreviewURL = %q, want %q", *r.PreviewURL, "https://preview.tardi.ai")
	}
}

func TestToInstanceResponse_Step(t *testing.T) {
	step := StepBootstrap
	inst := VpsInstance{
		ID:        uuid.New(),
		Name:      "agent",
		Provider:  "hetzner",
		Region:    "us-east",
		Status:    VpsStatusProvisioning,
		Step:      &step,
		CreatedAt: time.Now(),
	}

	r := ToInstanceResponse(inst)

	if r.Step == nil {
		t.Fatal("Step is nil, want non-nil")
	}
	if *r.Step != "bootstrap" {
		t.Errorf("Step = %q, want %q", *r.Step, "bootstrap")
	}
}

func TestToInstanceResponse_LastHeartbeatAt(t *testing.T) {
	hb := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	inst := VpsInstance{
		ID:              uuid.New(),
		Name:            "agent",
		Provider:        "hetzner",
		Region:          "us-east",
		Status:          VpsStatusActive,
		LastHeartbeatAt: &hb,
		CreatedAt:       time.Now(),
	}

	r := ToInstanceResponse(inst)

	if r.LastHeartbeatAt == nil {
		t.Fatal("LastHeartbeatAt is nil, want non-nil")
	}
	if *r.LastHeartbeatAt != hb.Format(time.RFC3339) {
		t.Errorf("LastHeartbeatAt = %q, want %q", *r.LastHeartbeatAt, hb.Format(time.RFC3339))
	}

	// nil case
	inst.LastHeartbeatAt = nil
	r2 := ToInstanceResponse(inst)
	if r2.LastHeartbeatAt != nil {
		t.Errorf("LastHeartbeatAt = %q, want nil", *r2.LastHeartbeatAt)
	}
}

func TestToInstanceResponse_AgentFields(t *testing.T) {
	inst := VpsInstance{
		ID:                uuid.New(),
		Name:              "agent",
		Provider:          "hetzner",
		Region:            "us-east",
		Status:            VpsStatusActive,
		AgentStatus:       strPtr("running"),
		AgentError:        strPtr("some error"),
		OpenClawAuthToken: strPtr("tok-abc123"),
		CreatedAt:         time.Now(),
	}

	r := ToInstanceResponse(inst)

	if r.AgentStatus == nil || *r.AgentStatus != "running" {
		t.Errorf("AgentStatus = %v, want %q", r.AgentStatus, "running")
	}
	if r.AgentError == nil || *r.AgentError != "some error" {
		t.Errorf("AgentError = %v, want %q", r.AgentError, "some error")
	}
	if r.OpenClawAuthToken == nil || *r.OpenClawAuthToken != "tok-abc123" {
		t.Errorf("OpenClawAuthToken = %v, want %q", r.OpenClawAuthToken, "tok-abc123")
	}
}

func TestToSnapshotResponse_Basic(t *testing.T) {
	id := uuid.New()
	instanceID := uuid.New()
	now := time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC)
	size := float32(20.5)

	s := Snapshot{
		ID:            id,
		VpsInstanceID: instanceID,
		Name:          "snap-1",
		Status:        SnapshotStatusReady,
		SizeGB:        &size,
		CreatedAt:     now,
	}

	r := ToSnapshotResponse(s)

	if r.ID != id.String() {
		t.Errorf("ID = %q, want %q", r.ID, id.String())
	}
	if r.InstanceID != instanceID.String() {
		t.Errorf("InstanceID = %q, want %q", r.InstanceID, instanceID.String())
	}
	if r.Name != "snap-1" {
		t.Errorf("Name = %q, want %q", r.Name, "snap-1")
	}
	if r.Status != "ready" {
		t.Errorf("Status = %q, want %q", r.Status, "ready")
	}
	if r.SizeGB == nil || *r.SizeGB != 20.5 {
		t.Errorf("SizeGB = %v, want 20.5", r.SizeGB)
	}
	if r.CreatedAt != now.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", r.CreatedAt, now.Format(time.RFC3339))
	}
}

func TestToSnapshotResponse_NilSizeGB(t *testing.T) {
	s := Snapshot{
		ID:            uuid.New(),
		VpsInstanceID: uuid.New(),
		Name:          "snap-2",
		Status:        SnapshotStatusCreating,
		SizeGB:        nil,
		CreatedAt:     time.Now(),
	}

	r := ToSnapshotResponse(s)

	if r.SizeGB != nil {
		t.Errorf("SizeGB = %v, want nil", r.SizeGB)
	}
}

func TestToSubscriptionResponse_Nil(t *testing.T) {
	r := ToSubscriptionResponse(nil)
	if r != nil {
		t.Errorf("got %v, want nil", r)
	}
}

func TestToSubscriptionResponse_Basic(t *testing.T) {
	periodEnd := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	sub := &Subscription{
		ID:                uuid.New(),
		UserID:            uuid.New(),
		PlanTier:          PlanStandard,
		Status:            SubStatusActive,
		CurrentPeriodEnd:  &periodEnd,
		CancelAtPeriodEnd: false,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	r := ToSubscriptionResponse(sub)

	if r == nil {
		t.Fatal("got nil, want non-nil")
	}
	if r.Plan != "standard" {
		t.Errorf("Plan = %q, want %q", r.Plan, "standard")
	}
	if r.Status != "active" {
		t.Errorf("Status = %q, want %q", r.Status, "active")
	}
	if r.CurrentPeriodEnd != periodEnd.Format(time.RFC3339) {
		t.Errorf("CurrentPeriodEnd = %q, want %q", r.CurrentPeriodEnd, periodEnd.Format(time.RFC3339))
	}
	if r.CancelAtPeriodEnd != false {
		t.Errorf("CancelAtPeriodEnd = %v, want false", r.CancelAtPeriodEnd)
	}
}

func TestToSubscriptionResponse_NilCurrentPeriodEnd(t *testing.T) {
	sub := &Subscription{
		ID:                uuid.New(),
		UserID:            uuid.New(),
		PlanTier:          PlanStandard,
		Status:            SubStatusActive,
		CurrentPeriodEnd:  nil,
		CancelAtPeriodEnd: true,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	r := ToSubscriptionResponse(sub)

	if r == nil {
		t.Fatal("got nil, want non-nil")
	}
	if r.CurrentPeriodEnd != "" {
		t.Errorf("CurrentPeriodEnd = %q, want empty string", r.CurrentPeriodEnd)
	}
}

func TestToSubscriptionResponse_CancelAtPeriodEnd(t *testing.T) {
	sub := &Subscription{
		ID:                uuid.New(),
		UserID:            uuid.New(),
		PlanTier:          PlanStandard,
		Status:            SubStatusActive,
		CancelAtPeriodEnd: true,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	r := ToSubscriptionResponse(sub)

	if r == nil {
		t.Fatal("got nil, want non-nil")
	}
	if r.CancelAtPeriodEnd != true {
		t.Errorf("CancelAtPeriodEnd = %v, want true", r.CancelAtPeriodEnd)
	}
}
