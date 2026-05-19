package models

import "testing"

func TestNormalizeHermesProviderModelDefaultsCodex(t *testing.T) {
	provider, model := NormalizeHermesProviderModel("openai-codex", "openai-codex/gpt-5.5")
	if provider != HermesDefaultProvider {
		t.Fatalf("provider = %q, want %q", provider, HermesDefaultProvider)
	}
	if model != HermesDefaultModel {
		t.Fatalf("model = %q, want %q", model, HermesDefaultModel)
	}
}

func TestNormalizeHermesProviderModelPreservesExplicitOpenRouter(t *testing.T) {
	provider, model := NormalizeHermesProviderModel("openrouter", "moonshotai/kimi-k2.5")
	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if model != "moonshotai/kimi-k2.5" {
		t.Fatalf("model = %q, want moonshotai/kimi-k2.5", model)
	}
}
