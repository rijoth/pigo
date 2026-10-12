package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	os.WriteFile(path, []byte(`{
	  "unknownTop": 1,
	  "providers": {
	    "nanogpt": {
	      "baseUrl": "https://nano-gpt.com/api/v1",
	      "api": "openai-completions",
	      "apiKey": "$NANO_KEY",
	      "unknownProvider": true,
	      "models": [
	        {"id": "qwen", "name": "Qwen", "reasoning": false, "input": ["text"], "unknownModel": 3},
	        {"id": "other"}
	      ]
	    },
	    "local": {
	      "models": [
	        {"id": "llama", "baseUrl": "http://localhost:8080/v1", "contextWindow": 4096},
	        {"id": "tweak", "reasoning": false}
	      ],
	      "modelOverrides": {"tweak": {"name": "Tweaked", "reasoning": true, "maxTokens": 100}}
	    }
	  }
	}`), 0o600)

	models, err := LoadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 4 {
		t.Fatalf("got %d models: %+v", len(models), models)
	}
	if models[0].Ref() != "local/llama" {
		t.Fatalf("first = %q", models[0].Ref())
	}
	qwen, ok := Find(models, "nanogpt/qwen")
	if !ok || qwen.BaseURL != "https://nano-gpt.com/api/v1" || qwen.Name != "Qwen" || qwen.API != "openai-completions" {
		t.Fatalf("qwen = %+v ok=%v", qwen, ok)
	}
	tweak, ok := Find(models, "tweak")
	if !ok || tweak.Name != "Tweaked" || !tweak.Reasoning || tweak.MaxTokens != 100 {
		t.Fatalf("tweak = %+v ok=%v", tweak, ok)
	}
	other, ok := Find(models, "other")
	if !ok || other.API != "openai-completions" || other.APIKey != "$NANO_KEY" {
		t.Fatalf("other = %+v ok=%v", other, ok)
	}
	if _, ok := Find(models, "missing"); ok {
		t.Fatal("missing model found")
	}
}

func TestLoadModelsMissingFile(t *testing.T) {
	models, err := LoadModels(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || models != nil {
		t.Fatalf("models=%v err=%v", models, err)
	}
}

func TestResolveKey(t *testing.T) {
	os.Setenv("PIGO_TEST_KEY", "sekret")
	defer os.Unsetenv("PIGO_TEST_KEY")
	cases := map[string]string{
		"$PIGO_TEST_KEY":   "sekret",
		"${PIGO_TEST_KEY}": "sekret",
		"$$PIGO_TEST_KEY":  "$PIGO_TEST_KEY",
		"literal":          "literal",
	}
	for src, want := range cases {
		if got := (ModelEntry{APIKey: src}).ResolveKey(); got != want {
			t.Errorf("ResolveKey(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestClearAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	os.WriteFile(path, []byte(`{
	  "providers": {
	    "opencode": {"baseUrl": "https://x", "apiKey": "secret", "models": [{"id": "glm"}]},
	    "nanogpt": {"apiKey": "$NANO_KEY", "models": [{"id": "qwen"}]}
	  }
	}`), 0o600)

	if err := ClearAPIKey(path, "opencode"); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Find(entries, "opencode/glm"); !ok {
		t.Fatalf("opencode model lost: %+v", entries)
	}
	if got, _ := Find(entries, "opencode/glm"); got.APIKey != "" {
		t.Fatalf("apiKey = %q, want empty", got.APIKey)
	}

	// $ENV sources are configuration, not stored secrets.
	if err := ClearAPIKey(path, "nanogpt"); err != nil {
		t.Fatal(err)
	}
	entries, err = LoadModels(path)
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := Find(entries, "nanogpt/qwen"); e.APIKey != "$NANO_KEY" {
		t.Fatalf("env apiKey = %q, want unchanged", e.APIKey)
	}

	if err := ClearAPIKey(filepath.Join(dir, "absent.json"), "opencode"); err != nil {
		t.Fatalf("missing file: %v", err)
	}
}
