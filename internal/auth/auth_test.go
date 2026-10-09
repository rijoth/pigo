package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	creds, err := Load(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("creds = %v", creds)
	}
}

func TestLoadMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	for _, body := range []string{
		"{",
		`{"anthropic": "nope"}`,
		`{"anthropic": {"type": "bogus", "key": "x"}}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("Load(%q) succeeded, want error", body)
		}
	}
}

func TestSetPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "auth.json")
	if err := Set(path, "anthropic", "sk-secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir mode = %o, want 700", got)
	}
	key, err := Key(path, "anthropic")
	if err != nil || key != "sk-secret" {
		t.Fatalf("Key = %q err = %v", key, err)
	}
}

func TestSetAtomicAndPreserves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := Set(path, "anthropic", "a"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "openai", "b"); err != nil {
		t.Fatal(err)
	}
	creds, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds["anthropic"].Key != "a" || creds["openai"].Key != "b" {
		t.Fatalf("creds = %+v", creds)
	}
	// The atomic write renames a temp file into place, leaving no temp behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "auth.json" {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestSetFailureLeavesFileIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not apply to root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := Set(path, "anthropic", "a"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := Set(path, "openai", "b"); err == nil {
		t.Fatal("Set into read-only dir succeeded, want error")
	}
	key, err := Key(path, "anthropic")
	if err != nil || key != "a" {
		t.Fatalf("original key lost: %q err = %v", key, err)
	}
}

func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := Set(path, "anthropic", "a"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "openai", "b"); err != nil {
		t.Fatal(err)
	}
	if err := Delete(path, "anthropic"); err != nil {
		t.Fatal(err)
	}
	if key, _ := Key(path, "anthropic"); key != "" {
		t.Fatalf("anthropic still stored: %q", key)
	}
	if key, _ := Key(path, "openai"); key != "b" {
		t.Fatalf("openai = %q", key)
	}
	// Deleting an absent provider is a no-op.
	if err := Delete(path, "missing"); err != nil {
		t.Fatal(err)
	}
}

func TestKeyResolution(t *testing.T) {
	t.Setenv("PIGO_AUTH_TEST_ENV", "from-env")
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	body := `{
	  "literal": {"type": "api_key", "key": "plain"},
	  "env": {"type": "api_key", "key": "$PIGO_AUTH_TEST_ENV"},
	  "braced": {"type": "api_key", "key": "${PIGO_AUTH_TEST_ENV}"},
	  "override": {"type": "api_key", "key": "$PIGO_AUTH_TEST_ENV", "env": {"PIGO_AUTH_TEST_ENV": "from-cred"}},
	  "command": {"type": "api_key", "key": "!printf cmd-key"},
	  "dollar": {"type": "api_key", "key": "sk-$5$$x"},
	  "missing": {"type": "api_key", "key": "$PIGO_AUTH_TEST_UNSET"},
	  "oauth": {"type": "oauth", "access": "x", "refresh": "y", "expires": 1}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"literal":  "plain",
		"env":      "from-env",
		"braced":   "from-env",
		"override": "from-cred",
		"command":  "cmd-key",
		"dollar":   "sk-$5$x",
		"missing":  "",
		"oauth":    "",
	}
	for provider, want := range cases {
		got, err := Key(path, provider)
		if err != nil {
			t.Fatalf("Key(%q): %v", provider, err)
		}
		if got != want {
			t.Errorf("Key(%q) = %q, want %q", provider, got, want)
		}
	}
}
