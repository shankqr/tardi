package race_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/shanq/tardi/internal/api"
	"github.com/shanq/tardi/internal/api/middleware"
	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

// TestConcurrentConfigUpdates verifies that concurrent config updates
// produce correct version numbers and don't lose data.
//
// BUG: The "preserve existing config fields" pattern reads existing config,
// then writes merged config. Two concurrent updates both read the same
// existing config, then write — the second overwrites the first's changes.
//
// Run with: go test -race -count=100 -run TestConcurrentConfigUpdates
func TestConcurrentConfigUpdates(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)

	// Seed initial config
	ac := &models.AgentConfig{
		ID:            uuid.New(),
		VpsInstanceID: inst.ID,
		Config:        map[string]any{"model": "initial-model"},
		Version:       1,
	}
	if err := db.CreateAgentConfig(context.Background(), pool, ac); err != nil {
		t.Fatalf("seed initial config: %v", err)
	}

	// Seed a valid model in the models table
	_, err := pool.Exec(context.Background(), `
		INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order)
		VALUES ('model-a', 'Model A', 'openrouter', 'standard', true, true, 1)
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		t.Fatalf("seed model: %v", err)
	}

	deps := makeDeps(t, pool)
	handler := api.UpdateAgentConfigHandler(deps)

	const concurrency = 5

	type result struct {
		status  int
		version float64
	}

	results := runConcurrent(concurrency, func(i int) result {
		config := map[string]any{
			"model": "model-a",
		}
		body, _ := json.Marshal(map[string]any{"config": config})

		req := httptest.NewRequest(http.MethodPut, "/api/instances/"+inst.ID.String()+"/config", bytes.NewReader(body))
		req.SetPathValue("id", inst.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		ver, _ := resp["version"].(float64)

		return result{status: w.Code, version: ver}
	})

	// All should succeed
	for i, r := range results {
		if r.status != http.StatusOK {
			t.Errorf("goroutine %d: expected 200, got %d", i, r.status)
		}
	}

	// Final version should be initial(1) + concurrency(5) = 6
	// Because CreateAgentConfig uses ON CONFLICT DO UPDATE SET version = version + 1
	finalConfig, err := db.GetAgentConfigByInstanceID(context.Background(), pool, inst.ID)
	if err != nil {
		t.Fatalf("get final config: %v", err)
	}

	expectedVersion := 1 + concurrency
	if finalConfig.Version != expectedVersion {
		t.Errorf("version mismatch: expected %d, got %d (lost updates due to race)", expectedVersion, finalConfig.Version)
	}
}

// TestConcurrentFieldConfigRace verifies that concurrent updates to different
// config fields don't lose each other's changes.
//
// BUG: UpdateAgentConfigHandler reads existing config to preserve null fields,
// then writes the merged result. Two concurrent updates (one setting custom_field,
// one setting model) will both read the pre-update config. The second write
// will overwrite the first's changes.
//
// Run with: go test -race -count=10 -run TestConcurrentFieldConfigRace
func TestConcurrentFieldConfigRace(t *testing.T) {
	pool := getTestPool(t)
	cleanupTables(t, pool)
	t.Cleanup(func() { cleanupTables(t, pool) })

	user := createTestUser(t, pool)
	sub := createTestSubscription(t, pool, user.ID)
	inst := createTestInstance(t, pool, user.ID, sub.ID, models.VpsStatusActive)

	// Seed initial config with both fields
	ac := &models.AgentConfig{
		ID:            uuid.New(),
		VpsInstanceID: inst.ID,
		Config: map[string]any{
			"model":              "old-model",
			"custom_field": "old-value",
			"openrouter_api_key": "existing-key",
		},
		Version: 1,
	}
	if err := db.CreateAgentConfig(context.Background(), pool, ac); err != nil {
		t.Fatalf("seed initial config: %v", err)
	}

	// Seed valid models
	_, err := pool.Exec(context.Background(), `
		INSERT INTO models (id, display_name, provider, tier, is_enabled, is_default, sort_order)
		VALUES ('new-model', 'New Model', 'openrouter', 'standard', true, true, 1)
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		t.Fatalf("seed model: %v", err)
	}

	deps := makeDeps(t, pool)
	handler := api.UpdateAgentConfigHandler(deps)

	type result struct {
		status int
		body   string
	}

	_ = runConcurrent(2, func(i int) result {
		var config map[string]any
		if i == 0 {
			// Goroutine 1: Update custom_field (leave model as nil to preserve)
			config = map[string]any{
				"custom_field": "new-custom-value",
			}
		} else {
			// Goroutine 2: Update model (leave custom_field as nil to preserve)
			config = map[string]any{
				"model": "new-model",
			}
		}

		body, _ := json.Marshal(map[string]any{"config": config})

		req := httptest.NewRequest(http.MethodPut, "/api/instances/"+inst.ID.String()+"/config", bytes.NewReader(body))
		req.SetPathValue("id", inst.ID.String())
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		return result{status: w.Code, body: w.Body.String()}
	})

	// Verify final config has BOTH changes
	finalConfig, err := db.GetAgentConfigByInstanceID(context.Background(), pool, inst.ID)
	if err != nil {
		t.Fatalf("get final config: %v", err)
	}

	customField, _ := finalConfig.Config["custom_field"].(string)
	model, _ := finalConfig.Config["model"].(string)

	// At least one of these will be wrong due to the race
	if customField != "new-custom-value" || model != "new-model" {
		t.Errorf("RACE CONDITION: config field was overwritten\n"+
			"  custom_field: got %q, want %q\n"+
			"  model: got %q, want %q\n"+
			"  Full config: %v",
			customField, "new-custom-value",
			model, "new-model",
			finalConfig.Config,
		)
	}
}
