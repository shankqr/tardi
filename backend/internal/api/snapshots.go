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

func CreateSnapshotHandler(deps Dependencies) http.HandlerFunc {
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
			slog.Error("create snapshot: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.Status != models.VpsStatusActive {
			WriteError(w, http.StatusConflict, "invalid_state", "instance must be active to create a snapshot")
			return
		}

		if inst.ProviderServerID == nil {
			WriteError(w, http.StatusConflict, "invalid_state", "instance has no provider server")
			return
		}

		// Enforce 3-snapshot limit
		count, err := db.CountActiveSnapshotsByInstanceID(r.Context(), deps.Pool, instanceID)
		if err != nil {
			slog.Error("create snapshot: count snapshots", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to check snapshot limit")
			return
		}
		if count >= 3 {
			WriteError(w, http.StatusConflict, "limit_reached", "maximum 3 snapshots allowed")
			return
		}

		snap := &models.Snapshot{
			ID:            uuid.New(),
			VpsInstanceID: instanceID,
			Name:          req.Name,
			Status:        models.SnapshotStatusCreating,
		}
		if err := db.CreateSnapshot(r.Context(), deps.Pool, snap); err != nil {
			slog.Error("create snapshot: insert", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create snapshot")
			return
		}

		if err := db.UpdateInstanceStatus(r.Context(), deps.Pool, instanceID, models.VpsStatusSnapshotting); err != nil {
			slog.Error("create snapshot: update instance status", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update instance status")
			return
		}

		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			executeCreateSnapshot(deps, inst, snap)
		}()

		_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
			ID:           uuid.New(),
			UserID:       user.ID,
			Action:       "create_snapshot",
			ResourceType: "snapshot",
			ResourceID:   &snap.ID,
		})

		slog.Info("snapshot creating", "snapshot_id", snap.ID, "instance_id", instanceID)
		snap.CreatedAt = time.Now()
		WriteJSON(w, http.StatusCreated, models.ToSnapshotResponse(*snap))
	}
}

func RestoreSnapshotHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		snapshotID, err := uuid.Parse(r.PathValue("snapshot_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid snapshot id")
			return
		}

		snap, err := db.GetSnapshotByID(r.Context(), deps.Pool, snapshotID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "snapshot not found")
				return
			}
			slog.Error("restore snapshot: get snapshot", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get snapshot")
			return
		}

		if snap.Status != models.SnapshotStatusReady {
			WriteError(w, http.StatusConflict, "invalid_state", "snapshot must be ready to restore")
			return
		}

		inst, err := db.GetInstanceByID(r.Context(), deps.Pool, snap.VpsInstanceID, user.ID)
		if err != nil {
			slog.Error("restore snapshot: get instance", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instance")
			return
		}

		if inst.Status != models.VpsStatusActive {
			WriteError(w, http.StatusConflict, "invalid_state", "instance must be active to restore a snapshot")
			return
		}

		if inst.ProviderServerID == nil {
			WriteError(w, http.StatusConflict, "invalid_state", "instance has no provider server")
			return
		}

		if err := db.UpdateInstanceStatus(r.Context(), deps.Pool, inst.ID, models.VpsStatusRestoring); err != nil {
			slog.Error("restore snapshot: update instance status", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update instance status")
			return
		}

		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			executeRestore(deps, inst, snap)
		}()

		_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
			ID:           uuid.New(),
			UserID:       user.ID,
			Action:       "restore_snapshot",
			ResourceType: "snapshot",
			ResourceID:   &snap.ID,
		})

		slog.Info("snapshot restoring", "snapshot_id", snapshotID, "instance_id", inst.ID)
		w.WriteHeader(http.StatusOK)
	}
}

func DeleteSnapshotHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
			return
		}

		snapshotID, err := uuid.Parse(r.PathValue("snapshot_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid snapshot id")
			return
		}

		snap, err := db.GetSnapshotByID(r.Context(), deps.Pool, snapshotID, user.ID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				WriteError(w, http.StatusNotFound, "not_found", "snapshot not found")
				return
			}
			slog.Error("delete snapshot: get snapshot", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get snapshot")
			return
		}

		if snap.Status != models.SnapshotStatusReady && snap.Status != models.SnapshotStatusError {
			WriteError(w, http.StatusConflict, "invalid_state", "snapshot cannot be deleted in current state")
			return
		}

		if err := db.UpdateSnapshotStatus(r.Context(), deps.Pool, snapshotID, models.SnapshotStatusDeleting); err != nil {
			slog.Error("delete snapshot: update status", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update snapshot status")
			return
		}

		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			executeDeleteSnapshot(deps, snap)
		}()

		_ = db.InsertAuditLog(r.Context(), deps.Pool, &models.AuditLogEntry{
			ID:           uuid.New(),
			UserID:       user.ID,
			Action:       "delete_snapshot",
			ResourceType: "snapshot",
			ResourceID:   &snap.ID,
		})

		slog.Info("snapshot deleting", "snapshot_id", snapshotID)
		w.WriteHeader(http.StatusOK)
	}
}

func executeCreateSnapshot(deps Dependencies, inst *models.VpsInstance, snap *models.Snapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	prov, err := deps.Registry.Get(inst.Provider)
	if err != nil {
		slog.Error("create snapshot: provider not found", "provider", inst.Provider, "error", err)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dbCancel()
		_ = db.UpdateSnapshotError(dbCtx, deps.Pool, snap.ID, "provider unavailable")
		_ = db.UpdateInstanceStatus(dbCtx, deps.Pool, inst.ID, models.VpsStatusActive)
		return
	}

	result, err := prov.CreateSnapshot(ctx, *inst.ProviderServerID, snap.Name)
	if err != nil {
		slog.Error("create snapshot: provider call failed", "snapshot_id", snap.ID, "error", err)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dbCancel()
		_ = db.UpdateSnapshotError(dbCtx, deps.Pool, snap.ID, err.Error())
		_ = db.UpdateInstanceStatus(dbCtx, deps.Pool, inst.ID, models.VpsStatusActive)
		return
	}

	// Fresh context for DB updates after potentially long provider call
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dbCancel()

	if err := db.UpdateSnapshotReady(dbCtx, deps.Pool, snap.ID, result.ProviderImageID, result.SizeGB); err != nil {
		slog.Error("create snapshot: store result", "snapshot_id", snap.ID, "error", err)
	}

	_ = db.UpdateInstanceStatus(dbCtx, deps.Pool, inst.ID, models.VpsStatusActive)
	slog.Info("snapshot created", "snapshot_id", snap.ID, "image_id", result.ProviderImageID)
}

func executeRestore(deps Dependencies, inst *models.VpsInstance, snap *models.Snapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	prov, err := deps.Registry.Get(inst.Provider)
	if err != nil {
		slog.Error("restore: provider not found", "provider", inst.Provider, "error", err)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dbCancel()
		_ = db.UpdateInstanceStatus(dbCtx, deps.Pool, inst.ID, models.VpsStatusError)
		return
	}

	newPassword, err := prov.RebuildServer(ctx, *inst.ProviderServerID, *snap.ProviderImageID)
	if err != nil {
		slog.Error("restore: provider call failed", "instance_id", inst.ID, "error", err)
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dbCancel()
		_ = db.UpdateInstanceStatus(dbCtx, deps.Pool, inst.ID, models.VpsStatusError)
		return
	}

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dbCancel()

	if newPassword != "" {
		if err := db.UpdateInstanceRootPassword(dbCtx, deps.Pool, inst.ID, newPassword); err != nil {
			slog.Error("restore: store password", "instance_id", inst.ID, "error", err)
		}
	}

	_ = db.UpdateInstanceStatus(dbCtx, deps.Pool, inst.ID, models.VpsStatusActive)
	slog.Info("restore completed", "instance_id", inst.ID, "snapshot_id", snap.ID)
}

func executeDeleteSnapshot(deps Dependencies, snap *models.Snapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// If the snapshot has a provider image, delete it
	if snap.ProviderImageID != nil {
		inst, err := getSnapshotInstance(ctx, deps, snap.VpsInstanceID)
		if err != nil {
			slog.Error("delete snapshot: get instance", "error", err)
			dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer dbCancel()
			_ = db.UpdateSnapshotError(dbCtx, deps.Pool, snap.ID, "failed to get instance")
			return
		}

		prov, err := deps.Registry.Get(inst.Provider)
		if err != nil {
			slog.Error("delete snapshot: provider not found", "provider", inst.Provider, "error", err)
			dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer dbCancel()
			_ = db.UpdateSnapshotError(dbCtx, deps.Pool, snap.ID, "provider unavailable")
			return
		}

		if err := prov.DeleteSnapshot(ctx, *snap.ProviderImageID); err != nil {
			slog.Error("delete snapshot: provider call failed", "snapshot_id", snap.ID, "error", err)
			dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer dbCancel()
			_ = db.UpdateSnapshotError(dbCtx, deps.Pool, snap.ID, err.Error())
			return
		}
	}

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dbCancel()
	_ = db.UpdateSnapshotStatus(dbCtx, deps.Pool, snap.ID, models.SnapshotStatusDeleted)
	slog.Info("snapshot deleted", "snapshot_id", snap.ID)
}

func getSnapshotInstance(ctx context.Context, deps Dependencies, instanceID uuid.UUID) (*models.VpsInstance, error) {
	inst := &models.VpsInstance{}
	err := deps.Pool.QueryRow(ctx, `
		SELECT id, provider, provider_server_id
		FROM vps_instances WHERE id = $1
	`, instanceID).Scan(&inst.ID, &inst.Provider, &inst.ProviderServerID)
	if err != nil {
		return nil, err
	}
	return inst, nil
}
