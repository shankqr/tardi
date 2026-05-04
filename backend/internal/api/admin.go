package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/db"
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
			ID                    string  `json:"id"`
			Name                  string  `json:"name"`
			OpenClawVersion       *string `json:"openclaw_version"`
			TargetOpenClawVersion *string `json:"target_openclaw_version"`
			OpenClawUpdateStatus  *string `json:"openclaw_update_status"`
			OpenClawUpdateError   *string `json:"openclaw_update_error"`
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
		body.Version = strings.TrimSpace(body.Version)
		if body.Version == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "version is required")
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

// AdminGetHermesVersionHandler returns the global target Hermes version and active Hermes instance statuses.
func AdminGetHermesVersionHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hermesVersion, err := db.GetGlobalHermesVersion(r.Context(), deps.Pool)
		if err != nil {
			slog.Error("admin: get hermes version", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get hermes version")
			return
		}

		instances, err := db.GetActiveInstancesByStatus(r.Context(), deps.Pool, "active")
		if err != nil {
			slog.Error("admin: get active instances", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get instances")
			return
		}

		type instanceVersion struct {
			ID                    string  `json:"id"`
			Name                  string  `json:"name"`
			OpenClawVersion       *string `json:"version"`
			TargetOpenClawVersion *string `json:"target_version"`
			OpenClawUpdateStatus  *string `json:"update_status"`
			OpenClawUpdateError   *string `json:"update_error"`
		}

		var versions []instanceVersion
		for _, inst := range instances {
			if inst.Framework != "hermes" {
				continue
			}
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
			"global_target_version": hermesVersion,
			"instances":             versions,
		})
	}
}

// AdminSetHermesVersionHandler sets the global target Hermes version.
func AdminSetHermesVersionHandler(deps Dependencies) http.HandlerFunc {
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
			WriteError(w, http.StatusBadRequest, "bad_request", "cannot set version to 'latest' — use an explicit version tag (e.g. v2026.4.8)")
			return
		}

		if err := db.SetGlobalHermesVersion(r.Context(), deps.Pool, body.Version); err != nil {
			slog.Error("admin: set hermes version", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to set hermes version")
			return
		}

		slog.Info("admin: global hermes version updated", "version", body.Version)
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
			"instance_id":             instanceID.String(),
			"target_openclaw_version": body.Version,
		})
	}
}
