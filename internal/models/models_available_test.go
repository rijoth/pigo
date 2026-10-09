package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAvailableModelsMergesBuiltins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{
	  "providers": {
	    "anthropic": {
	      "models": [{"id": "claude-sonnet-4-5", "name": "Custom Sonnet", "baseUrl": "http://example.test"}]
	    },
	    "nanogpt": {
	      "baseUrl": "https://nano-gpt.com/api/v1",
	      "models": [{"id": "qwen"}]
	    }
	  }
	}`), 0o600)

	models := AvailableModels()

	// A models.json entry beats the built-in with the same ref.
	e, ok := Find(models, "anthropic/claude-sonnet-4-5")
	if !ok || e.Name != "Custom Sonnet" || e.BaseURL != "http://example.test" {
		t.Fatalf("anthropic entry = %+v ok=%v", e, ok)
	}
	if _, ok := Find(models, "nanogpt/qwen"); !ok {
		t.Fatal("models.json-only entry missing")
	}
	// Built-ins not in models.json are still present.
	if _, ok := Find(models, "openai/gpt-5"); !ok {
		t.Fatal("built-in openai/gpt-5 missing")
	}

	// No duplicate for the overridden ref.
	count := 0
	for _, m := range models {
		if m.Ref() == "anthropic/claude-sonnet-4-5" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("anthropic/claude-sonnet-4-5 appears %d times", count)
	}
	for i := 1; i < len(models); i++ {
		if models[i-1].Ref() > models[i].Ref() {
			t.Fatalf("not sorted: %q before %q", models[i-1].Ref(), models[i].Ref())
		}
	}
}

func TestAvailableModelsBuiltinsOnly(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	models := AvailableModels()
	if len(models) != len(BuiltinModels) {
		t.Fatalf("got %d models, want %d built-ins", len(models), len(BuiltinModels))
	}
}
