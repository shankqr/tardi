package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

// AdminGetVersionHandler returns the global target version and all instance version statuses.
func AdminGetVersionHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		globalVersion, err := db.GetGlobalTargetVersion(r.Context(), deps.Pool)
		if err != nil {
			slog.Error("admin: get global version", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get global version")
			return
		}

		// Get all active instances with their version info
		instances, err := db.GetActiveInstancesByStatus(r.Context(), deps.Pool, "active")
		if err != nil {
			slog.Error("admin: get active instances", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instances")
			return
		}

		type instanceVersion struct {
			ID                   string  `json:"id"`
			Name                 string  `json:"name"`
			OpenClawVersion      *string `json:"openclaw_version"`
			TargetOpenClawVersion *string `json:"target_openclaw_version"`
			OpenClawUpdateStatus *string `json:"openclaw_update_status"`
			OpenClawUpdateError  *string `json:"openclaw_update_error"`
		}

		var versions []instanceVersion
		for _, inst := range instances {
			versions = append(versions, instanceVersion{
				ID:                    inst.ID.String(),
				Name:                  inst.Name,
				OpenClawVersion:       inst.OpenClawVersion,
				TargetOpenClawVersion: inst.TargetOpenClawVersion,
				OpenClawUpdateStatus:  inst.OpenClawUpdateStatus,
				OpenClawUpdateError:   inst.OpenClawUpdateError,
			})
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"global_target_version": globalVersion,
			"instances":             versions,
		})
	}
}

// AdminSetGlobalVersionHandler sets the global target OpenClaw version.
func AdminSetGlobalVersionHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid request body")
			return
		}
		if body.Version == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "version is required")
			return
		}
		if body.Version == "latest" {
			WriteError(w, http.StatusBadRequest, "bad_request", "cannot set version to 'latest' — use an explicit version tag (e.g. v0.9.2)")
			return
		}

		if err := db.SetGlobalTargetVersion(r.Context(), deps.Pool, body.Version); err != nil {
			slog.Error("admin: set global version", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to set global version")
			return
		}

		slog.Info("admin: global openclaw version updated", "version", body.Version)
		WriteJSON(w, http.StatusOK, map[string]any{
			"global_target_version": body.Version,
		})
	}
}

// AdminResetPasswordByIPHandler resets the root password for an instance identified by IP.
// Used for manual password recovery when SSH access is lost.
func AdminResetPasswordByIPHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IP       string `json:"ip"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid request body")
			return
		}
		if body.IP == "" || body.Password == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "ip and password are required")
			return
		}

		if err := db.UpdateInstanceRootPasswordByIP(r.Context(), deps.Pool, body.IP, body.Password); err != nil {
			slog.Error("admin: reset password by ip", "ip", body.IP, "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update password")
			return
		}

		slog.Info("admin: root password updated by ip", "ip", body.IP)
		WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

// AdminSetInstanceVersionHandler sets a per-instance target version override.
func AdminSetInstanceVersionHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instanceID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid instance id")
			return
		}

		var body struct {
			Version *string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid request body")
			return
		}

		if err := db.SetInstanceTargetVersion(r.Context(), deps.Pool, instanceID, body.Version); err != nil {
			slog.Error("admin: set instance version", "instance_id", instanceID, "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to set instance version")
			return
		}

		slog.Info("admin: instance openclaw version override set", "instance_id", instanceID, "version", body.Version)
		WriteJSON(w, http.StatusOK, map[string]any{
			"instance_id":            instanceID.String(),
			"target_openclaw_version": body.Version,
		})
	}
}

// AdminBuildGoldenImageHandler triggers a golden image build.
func AdminBuildGoldenImageHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.GoldenImageBuilder == nil {
			WriteError(w, http.StatusServiceUnavailable, "unavailable", "golden image builder not configured")
			return
		}

		// Check if there's already a build in progress
		images, err := db.ListGoldenImages(r.Context(), deps.Pool)
		if err != nil {
			slog.Error("admin: list golden images", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to check existing builds")
			return
		}
		for _, img := range images {
			if img.Status == models.GoldenImageBuilding {
				WriteError(w, http.StatusConflict, "conflict", "a golden image build is already in progress")
				return
			}
		}

		// Run build in background
		deps.BGTasks.Add(1)
		go func() {
			defer deps.BGTasks.Done()
			img, err := deps.GoldenImageBuilder.Build(r.Context())
			if err != nil {
				slog.Error("admin: golden image build failed", "error", err)
				return
			}
			slog.Info("admin: golden image build complete",
				"image_id", img.ID,
				"provider_image_id", img.ProviderImageID,
			)
		}()

		WriteJSON(w, http.StatusAccepted, map[string]any{
			"status":  "building",
			"message": "golden image build started in background",
		})
	}
}

// AdminListGoldenImagesHandler returns all golden images.
func AdminListGoldenImagesHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		images, err := db.ListGoldenImages(r.Context(), deps.Pool)
		if err != nil {
			slog.Error("admin: list golden images", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list golden images")
			return
		}

		type goldenImageResp struct {
			ID              string  `json:"id"`
			Provider        string  `json:"provider"`
			Region          string  `json:"region"`
			ServerType      string  `json:"server_type"`
			ProviderImageID string  `json:"provider_image_id"`
			OpenClawVersion string  `json:"openclaw_version"`
			Status          string  `json:"status"`
			CreatedAt       string  `json:"created_at"`
			ActivatedAt     *string `json:"activated_at,omitempty"`
			DeprecatedAt    *string `json:"deprecated_at,omitempty"`
		}

		var resp []goldenImageResp
		for _, img := range images {
			item := goldenImageResp{
				ID:              img.ID.String(),
				Provider:        img.Provider,
				Region:          img.Region,
				ServerType:      img.ServerType,
				ProviderImageID: img.ProviderImageID,
				OpenClawVersion: img.OpenClawVersion,
				Status:          string(img.Status),
				CreatedAt:       img.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}
			if img.ActivatedAt != nil {
				s := img.ActivatedAt.Format("2006-01-02T15:04:05Z")
				item.ActivatedAt = &s
			}
			if img.DeprecatedAt != nil {
				s := img.DeprecatedAt.Format("2006-01-02T15:04:05Z")
				item.DeprecatedAt = &s
			}
			resp = append(resp, item)
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"golden_images": resp,
		})
	}
}
