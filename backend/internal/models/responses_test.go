package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func strPtr(s string) *string { return &s }

func TestToInstanceResponse_AllFieldsPopulated(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	heartbeat := time.Date(2026, 1, 15, 10, 25, 0, 0, time.UTC)
	step := StepInstallAgent
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	inst := VpsInstance{
		ID:                   id,
		Name:                 "my-agent",
		Status:               VpsStatusActive,
		Step:                 &step,
		Provider:             "hetzner",
		IPv4:                 strPtr("1.2.3.4"),
		Region:               "us-east",
		AgentStatus:          strPtr("running"),
		AgentError:           strPtr("some error"),
		OpenClawAuthToken:    strPtr("tok-abc"),
		OpenClawVersion:      strPtr("1.2.3"),
		OpenClawUpdateStatus: strPtr("up_to_date"),
		Domain:               strPtr("agent1.tardi.ai"),
		PreviewDomain:        strPtr("preview-agent1.tardi.ai"),
		LastHeartbeatAt:      &heartbeat,
		CreatedAt:            now,
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
	if r.Step == nil || *r.Step != "install_agent" {
		t.Errorf("Step = %v, want %q", r.Step, "install_agent")
	}
	if r.Provider != "hetzner" {
		t.Errorf("Provider = %q, want %q", r.Provider, "hetzner")
	}
	if r.IPv4 == nil || *r.IPv4 != "1.2.3.4" {
		t.Errorf("IPv4 = %v, want %q", r.IPv4, "1.2.3.4")
	}
	if r.Region != "us-east" {
		t.Errorf("Region = %q, want %q", r.Region, "us-east")
	}
	if r.AgentStatus == nil || *r.AgentStatus != "running" {
		t.Errorf("AgentStatus = %v, want %q", r.AgentStatus, "running")
	}
	if r.AgentError == nil || *r.AgentError != "some error" {
		t.Errorf("AgentError = %v, want %q", r.AgentError, "some error")
	}
	if r.OpenClawAuthToken == nil || *r.OpenClawAuthToken != "tok-abc" {
		t.Errorf("OpenClawAuthToken = %v, want %q", r.OpenClawAuthToken, "tok-abc")
	}
	if r.OpenClawVersion == nil || *r.OpenClawVersion != "1.2.3" {
		t.Errorf("OpenClawVersion = %v, want %q", r.OpenClawVersion, "1.2.3")
	}
	if r.OpenClawUpdateStatus == nil || *r.OpenClawUpdateStatus != "up_to_date" {
		t.Errorf("OpenClawUpdateStatus = %v, want %q", r.OpenClawUpdateStatus, "up_to_date")
	}
	if r.DashboardURL == nil || *r.DashboardURL != "https://agent1.tardi.ai" {
		t.Errorf("DashboardURL = %v, want %q", r.DashboardURL, "https://agent1.tardi.ai")
	}
	if r.PreviewURL == nil || *r.PreviewURL != "https://preview-agent1.tardi.ai" {
		t.Errorf("PreviewURL = %v, want %q", r.PreviewURL, "https://preview-agent1.tardi.ai")
	}
	if r.LastHeartbeatAt == nil || *r.LastHeartbeatAt != heartbeat.Format(time.RFC3339) {
		t.Errorf("LastHeartbeatAt = %v, want %q", r.LastHeartbeatAt, heartbeat.Format(time.RFC3339))
	}
	if r.CreatedAt != now.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", r.CreatedAt, now.Format(time.RFC3339))
	}
	// RootPassword should always be nil in the response
	if r.RootPassword != nil {
		t.Errorf("RootPassword should be nil, got %v", r.RootPassword)
	}
}

func TestToInstanceResponse_MinimalFields(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	inst := VpsInstance{
		ID:        id,
		Name:      "minimal",
		Status:    VpsStatusRequested,
		Provider:  "stub",
		Region:    "eu-central",
		CreatedAt: now,
	}

	r := ToInstanceResponse(inst)

	if r.ID != id.String() {
		t.Errorf("ID = %q, want %q", r.ID, id.String())
	}
	if r.Name != "minimal" {
		t.Errorf("Name = %q, want %q", r.Name, "minimal")
	}
	if r.Status != "requested" {
		t.Errorf("Status = %q, want %q", r.Status, "requested")
	}
	if r.Step != nil {
		t.Errorf("Step should be nil, got %v", r.Step)
	}
	if r.IPv4 != nil {
		t.Errorf("IPv4 should be nil, got %v", r.IPv4)
	}
	if r.DashboardURL != nil {
		t.Errorf("DashboardURL should be nil for no domain and no IPv4, got %v", r.DashboardURL)
	}
	if r.PreviewURL != nil {
		t.Errorf("PreviewURL should be nil, got %v", r.PreviewURL)
	}
	if r.LastHeartbeatAt != nil {
		t.Errorf("LastHeartbeatAt should be nil, got %v", r.LastHeartbeatAt)
	}
	if r.AgentStatus != nil {
		t.Errorf("AgentStatus should be nil, got %v", r.AgentStatus)
	}
}

func TestToInstanceResponse_IPv4FallbackForDashboardURL(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	inst := VpsInstance{
		ID:        uuid.New(),
		Name:      "ipv4-only",
		Status:    VpsStatusActive,
		Provider:  "hetzner",
		IPv4:      strPtr("5.6.7.8"),
		Region:    "us-east",
		CreatedAt: now,
		// Domain is nil, so it should fall back to IPv4
	}

	r := ToInstanceResponse(inst)

	if r.DashboardURL == nil {
		t.Fatal("DashboardURL should not be nil when IPv4 is set")
	}
	if *r.DashboardURL != "https://5.6.7.8" {
		t.Errorf("DashboardURL = %q, want %q", *r.DashboardURL, "https://5.6.7.8")
	}
}

func TestToInstanceResponse_DomainTakesPrecedenceOverIPv4(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	inst := VpsInstance{
		ID:        uuid.New(),
		Name:      "domain-pref",
		Status:    VpsStatusActive,
		Provider:  "hetzner",
		IPv4:      strPtr("5.6.7.8"),
		Domain:    strPtr("myagent.tardi.ai"),
		Region:    "us-east",
		CreatedAt: now,
	}

	r := ToInstanceResponse(inst)

	if r.DashboardURL == nil {
		t.Fatal("DashboardURL should not be nil")
	}
	if *r.DashboardURL != "https://myagent.tardi.ai" {
		t.Errorf("DashboardURL = %q, want domain-based URL", *r.DashboardURL)
	}
}

func TestToInstanceResponse_EmptyDomainFallsBackToIPv4(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	inst := VpsInstance{
		ID:        uuid.New(),
		Name:      "empty-domain",
		Status:    VpsStatusActive,
		Provider:  "hetzner",
		IPv4:      strPtr("9.8.7.6"),
		Domain:    strPtr(""), // empty string
		Region:    "us-east",
		CreatedAt: now,
	}

	r := ToInstanceResponse(inst)

	if r.DashboardURL == nil {
		t.Fatal("DashboardURL should not be nil when IPv4 is set")
	}
	if *r.DashboardURL != "https://9.8.7.6" {
		t.Errorf("DashboardURL = %q, want IPv4 fallback", *r.DashboardURL)
	}
}

func TestToSnapshotResponse(t *testing.T) {
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	snapID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	instID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	size := float32(10.5)

	snap := Snapshot{
		ID:            snapID,
		VpsInstanceID: instID,
		Name:          "pre-upgrade",
		Status:        SnapshotStatusReady,
		SizeGB:        &size,
		CreatedAt:     now,
	}

	r := ToSnapshotResponse(snap)

	if r.ID != snapID.String() {
		t.Errorf("ID = %q, want %q", r.ID, snapID.String())
	}
	if r.InstanceID != instID.String() {
		t.Errorf("InstanceID = %q, want %q", r.InstanceID, instID.String())
	}
	if r.Name != "pre-upgrade" {
		t.Errorf("Name = %q, want %q", r.Name, "pre-upgrade")
	}
	if r.Status != "ready" {
		t.Errorf("Status = %q, want %q", r.Status, "ready")
	}
	if r.SizeGB == nil || *r.SizeGB != 10.5 {
		t.Errorf("SizeGB = %v, want 10.5", r.SizeGB)
	}
	if r.CreatedAt != now.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", r.CreatedAt, now.Format(time.RFC3339))
	}
}

func TestToSnapshotResponse_NilSizeGB(t *testing.T) {
	snap := Snapshot{
		ID:            uuid.New(),
		VpsInstanceID: uuid.New(),
		Name:          "no-size",
		Status:        SnapshotStatusCreating,
		SizeGB:        nil,
		CreatedAt:     time.Now(),
	}

	r := ToSnapshotResponse(snap)

	if r.SizeGB != nil {
		t.Errorf("SizeGB should be nil, got %v", r.SizeGB)
	}
}

func TestToSubscriptionResponse_WithSubscription(t *testing.T) {
	periodEnd := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	sub := &Subscription{
		PlanTier:          PlanStandard,
		Status:            SubStatusActive,
		CurrentPeriodEnd:  &periodEnd,
		CancelAtPeriodEnd: false,
	}

	r := ToSubscriptionResponse(sub)

	if r == nil {
		t.Fatal("response should not be nil")
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

func TestToSubscriptionResponse_NilSubscription(t *testing.T) {
	r := ToSubscriptionResponse(nil)
	if r != nil {
		t.Errorf("response should be nil for nil subscription, got %+v", r)
	}
}

func TestToSubscriptionResponse_NilCurrentPeriodEnd(t *testing.T) {
	sub := &Subscription{
		PlanTier:          PlanPro,
		Status:            SubStatusPastDue,
		CurrentPeriodEnd:  nil,
		CancelAtPeriodEnd: true,
	}

	r := ToSubscriptionResponse(sub)

	if r == nil {
		t.Fatal("response should not be nil")
	}
	if r.Plan != "pro" {
		t.Errorf("Plan = %q, want %q", r.Plan, "pro")
	}
	if r.Status != "past_due" {
		t.Errorf("Status = %q, want %q", r.Status, "past_due")
	}
	if r.CurrentPeriodEnd != "" {
		t.Errorf("CurrentPeriodEnd = %q, want empty string", r.CurrentPeriodEnd)
	}
	if r.CancelAtPeriodEnd != true {
		t.Errorf("CancelAtPeriodEnd = %v, want true", r.CancelAtPeriodEnd)
	}
}
