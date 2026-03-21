package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/shanq/tardi/internal/db"
)

// orModelInfo holds cached metadata from OpenRouter's API.
type orModelInfo struct {
	Name            string
	Description     string
	ContextLength   int
	PromptPrice     string
	CompletionPrice string
}

var (
	orCache     map[string]orModelInfo
	orCacheTime time.Time
	orCacheMu   sync.Mutex
	orCacheTTL  = 1 * time.Hour
)

// fetchOpenRouterModels fetches model metadata from OpenRouter's public API.
func fetchOpenRouterModels() (map[string]orModelInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	cache := make(map[string]orModelInfo, len(result.Data))
	for _, m := range result.Data {
		cache[m.ID] = orModelInfo{
			Name:            m.Name,
			Description:     m.Description,
			ContextLength:   m.ContextLength,
			PromptPrice:     m.Pricing.Prompt,
			CompletionPrice: m.Pricing.Completion,
		}
	}
	return cache, nil
}

// getORCache returns cached OpenRouter model data, refreshing if stale.
func getORCache() map[string]orModelInfo {
	orCacheMu.Lock()
	defer orCacheMu.Unlock()

	if orCache != nil && time.Since(orCacheTime) < orCacheTTL {
		return orCache
	}

	fresh, err := fetchOpenRouterModels()
	if err != nil {
		slog.Warn("failed to fetch OpenRouter models, using stale cache", "error", err)
		return orCache // may be nil on first failure
	}

	orCache = fresh
	orCacheTime = time.Now()
	return orCache
}

// ListModelsHandler returns the available model catalog enriched with OpenRouter metadata. No auth required.
func ListModelsHandler(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		models, err := db.ListEnabledModels(r.Context(), deps.Pool)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list models")
			return
		}

		orData := getORCache()

		type modelResponse struct {
			ID              string `json:"id"`
			DisplayName     string `json:"display_name"`
			Provider        string `json:"provider"`
			Tier            string `json:"tier"`
			IsDefault       bool   `json:"is_default"`
			Description     string `json:"description,omitempty"`
			ContextLength   int    `json:"context_length,omitempty"`
			PromptPrice     string `json:"prompt_price,omitempty"`
			CompletionPrice string `json:"completion_price,omitempty"`
		}

		var defaultModelID string
		out := make([]modelResponse, 0, len(models))
		for _, m := range models {
			resp := modelResponse{
				ID:          m.ID,
				DisplayName: m.DisplayName,
				Provider:    m.Provider,
				Tier:        m.Tier,
				IsDefault:   m.IsDefault,
			}

			if orData != nil {
				if info, ok := orData[m.ID]; ok {
					resp.Description = info.Description
					resp.ContextLength = info.ContextLength
					resp.PromptPrice = info.PromptPrice
					resp.CompletionPrice = info.CompletionPrice
				}
			}

			out = append(out, resp)
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
