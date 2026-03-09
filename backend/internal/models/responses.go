package models

import "time"

// DashboardStateResponse matches frontend's DashboardState type.
type DashboardStateResponse struct {
	Instances    []InstanceResponse    `json:"instances"`
	Subscription *SubscriptionResponse `json:"subscription"`
	PendingJobs  int                   `json:"pending_jobs"`
}

// InstanceResponse matches frontend's VpsInstance type.
type InstanceResponse struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Step            *string `json:"step,omitempty"`
	Provider        string  `json:"provider"`
	IPv4            *string `json:"ipv4"`
	Region          string  `json:"region"`
	RootPassword    *string `json:"root_password,omitempty"`
	LastHeartbeatAt *string `json:"last_heartbeat_at"`
	CreatedAt       string  `json:"created_at"`
}

// SubscriptionResponse matches frontend's Subscription type.
type SubscriptionResponse struct {
	Plan             string `json:"plan"`
	Status           string `json:"status"`
	CurrentPeriodEnd string `json:"current_period_end"`
}

// CreateInstanceRequest matches frontend's POST /api/instances body.
type CreateInstanceRequest struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

// ToInstanceResponse converts a VpsInstance model to the API response type.
func ToInstanceResponse(inst VpsInstance) InstanceResponse {
	r := InstanceResponse{
		ID:        inst.ID.String(),
		Name:      inst.Name,
		Status:    string(inst.Status),
		Provider:  inst.Provider,
		IPv4:      inst.IPv4,
		Region:    inst.Region,
		CreatedAt: inst.CreatedAt.Format(time.RFC3339),
	}
	r.RootPassword = inst.RootPassword
	if inst.Step != nil {
		s := string(*inst.Step)
		r.Step = &s
	}
	if inst.LastHeartbeatAt != nil {
		t := inst.LastHeartbeatAt.Format(time.RFC3339)
		r.LastHeartbeatAt = &t
	}
	return r
}

// ToSubscriptionResponse converts a Subscription model to the API response type.
func ToSubscriptionResponse(sub *Subscription) *SubscriptionResponse {
	if sub == nil {
		return nil
	}
	r := &SubscriptionResponse{
		Plan:   string(sub.PlanTier),
		Status: string(sub.Status),
	}
	if sub.CurrentPeriodEnd != nil {
		r.CurrentPeriodEnd = sub.CurrentPeriodEnd.Format(time.RFC3339)
	}
	return r
}
