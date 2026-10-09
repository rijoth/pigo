package providers

import (
	"path/filepath"
	"testing"

	"pigo/internal/auth"
	"pigo/internal/models"
)

func TestAuthenticatedModels(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.Set(authPath, "opencode", "oc_secret"); err != nil {
		t.Fatal(err)
	}
	models := []models.ModelEntry{
		{Provider: "opencode", Model: "glm-5.2", APIKey: "ignored"},
		{Provider: "opencode", Model: "gpt-5"},
		{Provider: "deepseek", Model: "deepseek-v4-pro"},
	}

	got := AuthenticatedModels(models, authPath)
	if len(got) != 2 {
		t.Fatalf("got %d models: %+v", len(got), got)
	}
	for _, e := range got {
		if e.Provider != "opencode" {
			t.Fatalf("unexpected provider %q", e.Provider)
		}
	}
}

// TestAuthenticatedModelsUsesModelsKey checks that a provider-level models.json
// apiKey is found even when the first entry carries no key, as store entries do.
func TestAuthenticatedModelsUsesModelsKey(t *testing.T) {
	t.Setenv("NANO_KEY", "sekret")
	authPath := filepath.Join(t.TempDir(), "auth.json")
	models := []models.ModelEntry{
		{Provider: "nanogpt", Model: "a"},                      // store entry, no key
		{Provider: "nanogpt", Model: "b", APIKey: "$NANO_KEY"}, // models.json entry
	}
	got := AuthenticatedModels(models, authPath)
	if len(got) != 2 {
		t.Fatalf("got %d models, want both", len(got))
	}
}
