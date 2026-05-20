package models

import "testing"

func TestNormalizeHermesProviderModelPreservesCodex(t *testing.T) {
	provider, model := NormalizeHermesProviderModel("openai-codex", "openai-codex/gpt-5.5")
	if provider != "openai-codex" {
		t.Fatalf("provider = %q, want openai-codex", provider)
	}
	if model != "openai-codex/gpt-5.5" {
		t.Fatalf("model = %q, want openai-codex/gpt-5.5", model)
	}
}

func TestNormalizeHermesProviderModelNormalizesCodexAlias(t *testing.T) {
	provider, model := NormalizeHermesProviderModel("codex", "codex/gpt-5.5")
	if provider != "openai-codex" {
		t.Fatalf("provider = %q, want openai-codex", provider)
	}
	if model != "openai-codex/gpt-5.5" {
		t.Fatalf("model = %q, want openai-codex/gpt-5.5", model)
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
