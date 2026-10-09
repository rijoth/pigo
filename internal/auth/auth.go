// Package auth stores and resolves API keys in pigo's auth.json, mirroring Pi's
// credential format: each provider maps to an object with a type, a key source
// (literal, $ENV/${ENV}, or !command), and an optional env override map.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Credential is one provider entry in auth.json.
type Credential struct {
	Type string            `json:"type"`
	Key  string            `json:"key,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

// Path returns $XDG_CONFIG_HOME/pigo/auth.json, defaulting to
// ~/.config/pigo/auth.json.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pigo", "auth.json")
}

// Load reads path. A missing file yields an empty map and no error. An
// unreadable or malformed file is an error; callers must not overwrite it.
func Load(path string) (map[string]Credential, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Credential{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]Credential{}, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("auth.json: %w", err)
	}
	out := make(map[string]Credential, len(raw))
	for id, entry := range raw {
		var c Credential
		if err := json.Unmarshal(entry, &c); err != nil {
			return nil, fmt.Errorf("auth.json: provider %q: %w", id, err)
		}
		switch c.Type {
		case "api_key", "oauth":
		default:
			return nil, fmt.Errorf("auth.json: provider %q: invalid credential type %q", id, c.Type)
		}
		out[id] = c
	}
	return out, nil
}

// Key returns the resolved API key stored for provider, or "" when no usable
// api_key credential exists. Resolution failures (empty command output, a
// missing $ENV reference) yield "".
func Key(path, provider string) (string, error) {
	creds, err := Load(path)
	if err != nil {
		return "", err
	}
	c, ok := creds[provider]
	if !ok || c.Type != "api_key" || c.Key == "" {
		return "", nil
	}
	return resolve(c.Key, c.Env), nil
}

// Set stores an api_key credential for provider, creating the directory 0700
// and the file 0600. The write is atomic: a temp file in the same directory is
// renamed over path, so a crash never leaves a truncated file.
func Set(path, provider, key string) error {
	creds, err := Load(path)
	if err != nil {
		return err
	}
	creds[provider] = Credential{Type: "api_key", Key: key}
	return save(path, creds)
}

// Delete removes provider's credential. It is a no-op when the file or entry
// is absent.
func Delete(path, provider string) error {
	creds, err := Load(path)
	if err != nil {
		return err
	}
	if _, ok := creds[provider]; !ok {
		return nil
	}
	delete(creds, provider)
	return save(path, creds)
}

func save(path string, creds map[string]Credential) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(dir, ".auth-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// resolve turns a key source into a value: a leading !command, an $ENV/${ENV}
// template, or a literal.
func resolve(s string, env map[string]string) string {
	if strings.HasPrefix(s, "!") {
		out, err := exec.Command("sh", "-c", s[1:]).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if v, ok := expand(s, env); ok {
		return v
	}
	return ""
}

// expand interpolates $NAME and ${NAME}. ok is false when a referenced
// variable is unset, matching Pi (an unresolved value is treated as no key).
func expand(s string, env map[string]string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			b.WriteByte('$')
			i++
			continue
		}
		switch s[i+1] {
		case '$':
			b.WriteByte('$')
			i += 2
		case '!':
			b.WriteByte('!')
			i += 2
		case '{':
			j := strings.IndexByte(s[i+2:], '}')
			if j < 0 {
				b.WriteByte('$')
				i++
				continue
			}
			name := s[i+2 : i+2+j]
			if !validName(name) {
				b.WriteString(s[i : i+2+j+1])
				i += j + 3
				continue
			}
			v, ok := lookupEnv(env, name)
			if !ok {
				return "", false
			}
			b.WriteString(v)
			i += j + 3
		default:
			if i+1 >= len(s) || !isNameStart(s[i+1]) {
				b.WriteByte('$')
				i++
				continue
			}
			j := i + 1
			for j < len(s) && isNameByte(s[j]) {
				j++
			}
			v, ok := lookupEnv(env, s[i+1:j])
			if !ok {
				return "", false
			}
			b.WriteString(v)
			i = j
		}
	}
	return b.String(), true
}

func lookupEnv(env map[string]string, name string) (string, bool) {
	if v, ok := env[name]; ok && v != "" {
		return v, true
	}
	return os.LookupEnv(name)
}

func validName(s string) bool {
	if s == "" || !isNameStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isNameByte(s[i]) {
			return false
		}
	}
	return true
}

func isNameStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isNameByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}
