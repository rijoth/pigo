package providers

import (
	"os"
	"path/filepath"
	"testing"

	"pigo/internal/auth"
	"pigo/internal/models"
)

func TestResolveKeyOrder(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	entry := models.ModelEntry{Provider: "anthropic", APIKey: "from-models"}

	t.Setenv("ANTHROPIC_API_KEY", "")
	if got, err := ResolveKey("anthropic", entry, authPath); err != nil || got != "from-models" {
		t.Fatalf("models source = %q err = %v", got, err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	if got, err := ResolveKey("anthropic", entry, authPath); err != nil || got != "from-env" {
		t.Fatalf("env source = %q err = %v", got, err)
	}

	if err := auth.Set(authPath, "anthropic", "from-auth"); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveKey("anthropic", entry, authPath); err != nil || got != "from-auth" {
		t.Fatalf("auth source = %q err = %v", got, err)
	}
}

func TestResolveKeyNoSources(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	got, err := ResolveKey("anthropic", models.ModelEntry{}, filepath.Join(t.TempDir(), "auth.json"))
	if err != nil || got != "" {
		t.Fatalf("ResolveKey = %q err = %v", got, err)
	}
}

func TestProvidersMerge(t *testing.T) {
	dir := t.TempDir()
	pigoDir := filepath.Join(dir, "pigo")
	if err := os.MkdirAll(pigoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pigoDir, "models.json"), []byte(`{
	  "providers": {
	    "nanogpt": {"apiKey": "$NANO_KEY", "models": [{"id": "qwen"}]}
	  }
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	ps := Providers()
	ids := make(map[string]ProviderInfo, len(ps))
	for _, p := range ps {
		ids[p.ID] = p
	}
	if _, ok := ids["anthropic"]; !ok {
		t.Fatal("anthropic built-in missing")
	}
	if p, ok := ids["nanogpt"]; !ok || p.EnvVar != "" {
		t.Fatalf("nanogpt = %+v ok = %v", p, ok)
	}
	if p, ok := ids["openai-codex"]; !ok || !p.OAuth {
		t.Fatalf("openai-codex = %+v ok = %v", p, ok)
	}
	if p, ok := ids["opencode-go"]; !ok || p.EnvVar != "OPENCODE_API_KEY" || p.OAuth {
		t.Fatalf("opencode-go = %+v ok = %v", p, ok)
	}
	if EnvVar("deepseek") != "DEEPSEEK_API_KEY" {
		t.Fatalf("deepseek env = %q", EnvVar("deepseek"))
	}
}
