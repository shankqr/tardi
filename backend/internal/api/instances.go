package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

func CreateInstanceHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		var req models.CreateInstanceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid request body")
			return
		}
		if req.Name == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "name is required")
			return
		}
		if req.Region == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "region is required")
			return
		}
		// Default framework to openclaw if not provided.
		if req.Framework == "" {
			req.Framework = string(models.FrameworkOpenClaw)
		}
		switch models.AgentFramework(req.Framework) {
		case models.FrameworkOpenClaw, models.FrameworkHermes:
			// valid
		default:
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid framework: "+req.Framework)
			return
		}

		// Check active subscription
		sub, err := db.GetSubscriptionByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			slog.Error("create instance: get subscription", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to check subscription")
			return
		}
		if sub == nil || sub.Status != models.SubStatusActive {
			WriteError(w, http.StatusForbidden, "no_subscription", "active subscription required")
			return
		}

		// Enforce 1-agent limit
		count, err := db.CountActiveInstancesByUserID(r.Context(), deps.Pool, user.ID)
		if err != nil {
			slog.Error("create instance: count instances", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to check instance limit")
			return
		}
		if count >= 1 {
			WriteError(w, http.StatusConflict, "limit_reached", "maximum 1 active agent allowed")
			return
		}

		// Get provider mapping
		mapping, err := db.GetBestProviderMapping(r.Context(), deps.Pool, sub.PlanTier, req.Region)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusBadRequest, "bad_request", "no provider available for region: "+req.Region)
				return
			}
			slog.Error("create instance: get provider mapping", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to select provider")
			return
		}

		// Create instance
		instanceID := uuid.New()
		now := time.Now()
		inst := &models.VpsInstance{
			ID:             instanceID,
			UserID:         user.ID,
			SubscriptionID: sub.ID,
			Framework:      models.AgentFramework(req.Framework),
			Provider:       mapping.Provider,
			ProviderRegion: &mapping.ProviderRegion,
			Name:           req.Name,
			Region:         req.Region,
			Status:         models.VpsStatusRequested,
			CreatedAt:      now,
		}
		if err := db.CreateInstance(r.Context(), deps.Pool, inst); err != nil {
			if db.IsUniqueViolation(err) {
				WriteError(w, http.StatusConflict, "limit_reached", "maximum 1 active agent allowed")
				return
			}
			slog.Error("create instance: insert", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create instance")
			return
		}

		// Create provisioning job
		job := &models.ProvisioningJob{
			ID:             uuid.New(),
			VpsInstanceID:  instanceID,
			IdempotencyKey: "provision-" + instanceID.String(),
			Status:         models.JobPending,
			MaxAttempts:    5,
		}
		step := models.StepSelectProvider
		job.Step = &step
		if err := db.CreateProvisioningJob(r.Context(), deps.Pool, job); err != nil {
			slog.Error("create instance: create job", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create provisioning job")
			return
		}

		// Audit log
		_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
			ID:           uuid.New(),
			UserID:       user.ID,
			Action:       "create",
			ResourceType: "instance",
			ResourceID:   &instanceID,
		})

		slog.Info("instance created", "instance_id", instanceID, "user_id", user.ID, "provider", mapping.Provider)

		// Return the created instance
		WriteJSON(w, http.StatusCreated, models.ToInstanceResponse(*inst))
	}
}

func RestartInstanceHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("restart instance: get", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if err := db.UpdateInstanceStatusConditional(r.Context(), deps.Pool, instanceID, models.VpsStatusActive, models.VpsStatusRestarting); err != nil {
			if errors.Is(err, db.ErrConflict) {
				WriteError(w, http.StatusConflict, "invalid_state", "instance must be active to restart")
				return
			}
			slog.Error("restart instance: update status", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to restart instance")
			return
		}

		// Execute restart in background goroutine
		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			executeRestart(deps, inst)
		}()

		// Audit log
		_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
			ID:           uuid.New(),
			UserID:       user.ID,
			Action:       "restart",
			ResourceType: "instance",
			ResourceID:   &instanceID,
		})

		slog.Info("instance restarting", "instance_id", instanceID)
		w.WriteHeader(http.StatusOK)
	}
}

func DeleteInstanceHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("delete instance: get", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		excludeStatuses := []models.VpsStatus{models.VpsStatusTerminating, models.VpsStatusTerminated}
		if err := db.UpdateInstanceStatusConditionalNot(r.Context(), deps.Pool, instanceID, excludeStatuses, models.VpsStatusTerminating); err != nil {
			if errors.Is(err, db.ErrConflict) {
				WriteError(w, http.StatusConflict, "invalid_state", "instance is already being terminated")
				return
			}
			slog.Error("delete instance: update status", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to terminate instance")
			return
		}

		// Execute deletion in background goroutine
		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			executeDelete(deps, inst)
		}()

		// Audit log
		_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
			ID:           uuid.New(),
			UserID:       user.ID,
			Action:       "terminate",
			ResourceType: "instance",
			ResourceID:   &instanceID,
		})

		slog.Info("instance terminating", "instance_id", instanceID)
		w.WriteHeader(http.StatusOK)
	}
}

func RenameInstanceHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid request body")
			return
		}
		if req.Name == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "name is required")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("rename instance: get", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if err := db.UpdateInstanceName(r.Context(), deps.Pool, instanceID, req.Name); err != nil {
			slog.Error("rename instance: update", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to rename instance")
			return
		}

		inst.Name = req.Name
		slog.Info("instance renamed", "instance_id", instanceID, "name", req.Name)
		WriteJSON(w, http.StatusOK, models.ToInstanceResponse(*inst))
	}
}

func ResetPasswordHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, instanceID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "instance not found")
				return
			}
			slog.Error("reset password: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.Status != models.VpsStatusActive {
			WriteError(w, http.StatusConflict, "invalid_state", "instance must be active to reset password")
			return
		}

		if inst.ProviderServerID == nil {
			WriteError(w, http.StatusConflict, "invalid_state", "instance has no provider server")
			return
		}

		prov, err := deps.Registry.Get(inst.Provider)
		if err != nil {
			slog.Error("reset password: provider not found", "provider", inst.Provider, "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "provider unavailable")
			return
		}

		newPassword, err := prov.ResetPassword(r.Context(), *inst.ProviderServerID)
		if err != nil {
			slog.Error("reset password: provider call failed", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to reset password")
			return
		}

		if err := db.UpdateInstanceRootPassword(r.Context(), deps.Pool, instanceID, newPassword); err != nil {
			slog.Error("reset password: store password", "instance_id", inst.ID, "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to store new password")
			return
		}

		_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
			ID:           uuid.New(),
			UserID:       user.ID,
			Action:       "reset_password",
			ResourceType: "instance",
			ResourceID:   &instanceID,
		})

		slog.Info("password reset", "instance_id", instanceID)
		WriteJSON(w, http.StatusOK, map[string]string{"root_password": newPassword})
	}
}

// executeRestart calls the provider to restart the server and updates status.
func executeRestart(deps Dependencies, inst *models.VpsInstance) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if inst.ProviderServerID == nil {
		slog.Error("restart: no provider server ID", "instance_id", inst.ID)
		_ = db.UpdateInstanceStatus(ctx, deps.Pool, inst.ID, models.VpsStatusError)
		return
	}

	prov, err := deps.Registry.Get(inst.Provider)
	if err != nil {
		slog.Error("restart: provider not found", "provider", inst.Provider, "error", err)
		_ = db.UpdateInstanceStatus(ctx, deps.Pool, inst.ID, models.VpsStatusError)
		return
	}

	if err := prov.RestartServer(ctx, *inst.ProviderServerID); err != nil {
		slog.Error("restart: provider call failed", "instance_id", inst.ID, "error", err)
		_ = db.UpdateInstanceStatus(ctx, deps.Pool, inst.ID, models.VpsStatusError)
		return
	}

	// Wait for server to come back up (poll every 5s, up to 2 minutes)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	deadline := time.After(2 * time.Minute)

	for {
		select {
		case <-deadline:
			slog.Warn("restart: timeout waiting for server to come back", "instance_id", inst.ID)
			_ = db.UpdateInstanceStatus(ctx, deps.Pool, inst.ID, models.VpsStatusActive)
			return
		case <-ticker.C:
			server, err := prov.GetServer(ctx, *inst.ProviderServerID)
			if err != nil {
				slog.Warn("restart: poll failed", "instance_id", inst.ID, "error", err)
				continue
			}
			if server.Status == "running" {
				_ = db.UpdateInstanceStatus(ctx, deps.Pool, inst.ID, models.VpsStatusActive)
				slog.Info("restart: completed", "instance_id", inst.ID)
				return
			}
		}
	}
}

// executeDelete calls the provider to delete the server, cleans up DNS, and updates status.
func executeDelete(deps Dependencies, inst *models.VpsInstance) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if inst.ProviderServerID != nil {
		prov, err := deps.Registry.Get(inst.Provider)
		if err != nil {
			slog.Error("delete: provider not found", "provider", inst.Provider, "error", err)
		} else if err := prov.DeleteServer(ctx, *inst.ProviderServerID); err != nil {
			slog.Error("delete: provider call failed", "instance_id", inst.ID, "error", err)
			// Still mark terminated — we don't want orphaned instances in our DB
		}
	}

	// Clean up Cloudflare DNS records
	if deps.DNSClient != nil && inst.DNSRecordID != nil && *inst.DNSRecordID != "" {
		if err := deps.DNSClient.DeleteRecord(ctx, *inst.DNSRecordID); err != nil {
			slog.Error("delete: DNS cleanup failed", "instance_id", inst.ID, "dns_record_id", *inst.DNSRecordID, "error", err)
			// Non-fatal: orphaned DNS record is harmless (points to deleted IP)
		} else {
			slog.Info("delete: DNS record removed", "instance_id", inst.ID, "domain", inst.Domain)
		}
	}
	if deps.DNSClient != nil && inst.PreviewDNSRecordID != nil && *inst.PreviewDNSRecordID != "" {
		if err := deps.DNSClient.DeleteRecord(ctx, *inst.PreviewDNSRecordID); err != nil {
			slog.Error("delete: preview DNS cleanup failed", "instance_id", inst.ID, "error", err)
		} else {
			slog.Info("delete: preview DNS record removed", "instance_id", inst.ID, "domain", inst.PreviewDomain)
		}
	}

	_ = db.UpdateInstanceStatus(ctx, deps.Pool, inst.ID, models.VpsStatusTerminated)
	slog.Info("delete: completed", "instance_id", inst.ID)
}
