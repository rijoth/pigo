package models

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAvailableModelsMergesStore checks that Pi's cached provider catalog is
// merged in, with models.json winning on a duplicate ref.
func TestAvailableModelsMergesStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	pigoDir := filepath.Join(dir, "pigo")
	if err := os.MkdirAll(pigoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pigoDir, "models.json"), []byte(`{
	  "providers": {
	    "opencode": {
	      "baseUrl": "https://example.test/user",
	      "models": [{"id": "glm-5.2", "name": "Custom GLM"}]
	    }
	  }
	}`), 0o600)
	os.WriteFile(filepath.Join(pigoDir, "models-store.json"), []byte(`{
	  "opencode": {
	    "models": [
	      {"id": "glm-5.2", "name": "Store GLM", "baseUrl": "https://opencode.ai/zen/v1"},
	      {"id": "gpt-5", "name": "GPT-5", "api": "openai-responses"}
	    ],
	    "checkedAt": 123
	  },
	  "deepseek": {
	    "models": [{"id": "deepseek-v4-pro", "baseUrl": "https://api.deepseek.com"}]
	  }
	}`), 0o600)

	models := AvailableModels()

	// models.json overrides the store entry with the same ref.
	e, ok := Find(models, "opencode/glm-5.2")
	if !ok || e.Name != "Custom GLM" || e.BaseURL != "https://example.test/user" {
		t.Fatalf("glm-5.2 = %+v ok = %v", e, ok)
	}
	// Store-only models are present.
	if e, ok := Find(models, "opencode/gpt-5"); !ok || e.API != "openai-responses" {
		t.Fatalf("opencode/gpt-5 = %+v ok = %v", e, ok)
	}
	if _, ok := Find(models, "deepseek/deepseek-v4-pro"); !ok {
		t.Fatal("store-only provider missing")
	}
}

// TestLoadModelsStoreMissing checks a missing store file is not an error.
func TestLoadModelsStoreMissing(t *testing.T) {
	models, err := LoadModelsStore(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || models != nil {
		t.Fatalf("models = %v err = %v", models, err)
	}
}

// TestBuiltinOpenCodeGo checks the opencode-go default carries its endpoint.
func TestBuiltinOpenCodeGo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e, ok := Find(AvailableModels(), "opencode-go/glm-5.2")
	if !ok || e.BaseURL != "https://opencode.ai/zen/go/v1" || e.API != "openai-completions" {
		t.Fatalf("opencode-go/glm-5.2 = %+v ok = %v", e, ok)
	}
}
