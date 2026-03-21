package api

import (
	"net/http"

	"github.com/shanq/tardi/internal/db"
)

// ListModelsHandler returns the available model catalog. No auth required.
func ListModelsHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		models, err := db.ListEnabledModels(r.Context(), deps.Pool)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list models")
			return
		}

		type modelResponse struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Provider    string `json:"provider"`
			Tier        string `json:"tier"`
			IsDefault   bool   `json:"is_default"`
		}

		var defaultModelID string
		out := make([]modelResponse, 0, len(models))
		for _, m := range models {
			out = append(out, modelResponse{
				ID:          m.ID,
				DisplayName: m.DisplayName,
				Provider:    m.Provider,
				Tier:        m.Tier,
				IsDefault:   m.IsDefault,
			})
			if m.IsDefault {
				defaultModelID = m.ID
			}
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"models":           out,
			"default_model_id": defaultModelID,
		})
	}
}
