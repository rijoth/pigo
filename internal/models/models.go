package models

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ModelEntry is one provider/model pair from Pi's models.json or cached
// models-store.json, with provider defaults and modelOverrides merged in.
// Fields are exactly the ones in Pi's schema; unknown fields are ignored.
type ModelEntry struct {
	Provider      string
	Model         string
	Name          string
	BaseURL       string
	API           string
	APIKey        string // source: literal, $ENV/${ENV}, or !command
	Headers       map[string]string
	Compat        json.RawMessage
	Reasoning     bool
	Input         []string
	ContextWindow int
	MaxTokens     int
}

// Ref is the canonical "provider/model" reference.
func (e ModelEntry) Ref() string { return e.Provider + "/" + e.Model }

type modelDefinition struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	API           string            `json:"api"`
	BaseURL       string            `json:"baseUrl"`
	Reasoning     bool              `json:"reasoning"`
	Input         []string          `json:"input"`
	ContextWindow int               `json:"contextWindow"`
	MaxTokens     int               `json:"maxTokens"`
	Headers       map[string]string `json:"headers"`
	Compat        json.RawMessage   `json:"compat"`
}

type modelOverride struct {
	Name          *string           `json:"name"`
	Reasoning     *bool             `json:"reasoning"`
	Input         []string          `json:"input"`
	ContextWindow *int              `json:"contextWindow"`
	MaxTokens     *int              `json:"maxTokens"`
	Headers       map[string]string `json:"headers"`
	Compat        json.RawMessage   `json:"compat"`
}

type providerConfig struct {
	Name           string                   `json:"name"`
	BaseURL        string                   `json:"baseUrl"`
	APIKey         string                   `json:"apiKey"`
	API            string                   `json:"api"`
	Headers        map[string]string        `json:"headers"`
	Compat         json.RawMessage          `json:"compat"`
	Models         []modelDefinition        `json:"models"`
	ModelOverrides map[string]modelOverride `json:"modelOverrides"`
}

type modelsFile struct {
	Providers map[string]providerConfig `json:"providers"`
}

// BuiltinModels are the provider/model pairs pigo can reach with only the
// provider's standard API-key environment variable. They keep the selector
// useful before any models.json exists.
var BuiltinModels = []ModelEntry{
	{Provider: "anthropic", Model: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5"},
	{Provider: "openai", Model: "gpt-5", Name: "GPT-5"},
	{Provider: "google", Model: "gemini-2.5-pro", Name: "Gemini 2.5 Pro"},
	{Provider: "openrouter", Model: "anthropic/claude-sonnet-4.5", Name: "Claude Sonnet 4.5 (OpenRouter)"},
	{Provider: "opencode-go", Model: "glm-5.2", Name: "GLM-5.2 (OpenCode Go)", BaseURL: "https://opencode.ai/zen/go/v1", API: "openai-completions"},
}

// configDir returns pigo's config directory, $XDG_CONFIG_HOME/pigo or
// ~/.config/pigo, the same directory that holds auth.json and skills.
func configDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pigo")
}

// ModelsPath returns the path to pigo's models.json.
func ModelsPath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "models.json")
}

// ModelsStorePath returns the path to pigo's models-store.json, the cached
// provider catalog written when a provider's model list is refreshed. It
// lives beside models.json and is keyed by provider id.
func ModelsStorePath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "models-store.json")
}

// LoadModels reads a Pi models.json and returns a flat list of provider/model
// entries. A missing file yields an empty list and no error.
func LoadModels(path string) ([]ModelEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f modelsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("models.json: %w", err)
	}
	out := flattenProviders(f.Providers)
	sortModels(out)
	return out, nil
}

// LoadModelsStore reads Pi's cached provider catalog. The file maps provider
// id to an object with the same models schema as models.json, so it reuses
// providerConfig. A missing file yields an empty list and no error.
func LoadModelsStore(path string) ([]ModelEntry, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f map[string]providerConfig
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("models-store.json: %w", err)
	}
	out := flattenProviders(f)
	sortModels(out)
	return out, nil
}

// flattenProviders turns provider configs into a flat list of model entries,
// applying each provider's defaults and modelOverrides.
func flattenProviders(providers map[string]providerConfig) []ModelEntry {
	var out []ModelEntry
	for name, p := range providers {
		for _, m := range p.Models {
			if m.ID == "" {
				continue
			}
			e := ModelEntry{
				Provider:      name,
				Model:         m.ID,
				Name:          cmp.Or(m.Name, m.ID),
				BaseURL:       cmp.Or(m.BaseURL, p.BaseURL),
				API:           cmp.Or(m.API, p.API),
				APIKey:        p.APIKey,
				Headers:       mergeHeaders(p.Headers, m.Headers),
				Compat:        firstRaw(m.Compat, p.Compat),
				Reasoning:     m.Reasoning,
				Input:         m.Input,
				ContextWindow: m.ContextWindow,
				MaxTokens:     m.MaxTokens,
			}
			if o, ok := p.ModelOverrides[m.ID]; ok {
				if o.Name != nil {
					e.Name = *o.Name
				}
				if o.Reasoning != nil {
					e.Reasoning = *o.Reasoning
				}
				if o.Input != nil {
					e.Input = o.Input
				}
				if o.ContextWindow != nil {
					e.ContextWindow = *o.ContextWindow
				}
				if o.MaxTokens != nil {
					e.MaxTokens = *o.MaxTokens
				}
				e.Headers = mergeHeaders(e.Headers, o.Headers)
				if len(o.Compat) > 0 {
					e.Compat = o.Compat
				}
			}
			out = append(out, e)
		}
	}
	return out
}

func sortModels(models []ModelEntry) {
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Model < models[j].Model
	})
}

// DefaultModels loads Pi's models.json, returning nil on any error.
func DefaultModels() []ModelEntry {
	models, _ := LoadModels(ModelsPath())
	return models
}

// DefaultModelsStore loads Pi's models-store.json, returning nil on any error.
func DefaultModelsStore() []ModelEntry {
	models, _ := LoadModelsStore(ModelsStorePath())
	return models
}

// AvailableModels merges Pi's user config (models.json), Pi's cached provider
// catalog (models-store.json), and BuiltinModels. Earlier sources win on a
// duplicate provider/model, so a models.json entry can override the catalog
// and a catalog entry can override a built-in's base URL, key, or headers.
func AvailableModels() []ModelEntry {
	sources := [][]ModelEntry{DefaultModels(), DefaultModelsStore(), BuiltinModels}
	total := 0
	for _, s := range sources {
		total += len(s)
	}
	seen := make(map[string]bool, total)
	out := make([]ModelEntry, 0, total)
	for _, s := range sources {
		for _, e := range s {
			if seen[e.Ref()] {
				continue
			}
			seen[e.Ref()] = true
			out = append(out, e)
		}
	}
	sortModels(out)
	return out
}

// ContextWindow returns the known context size for ref, or 0 when unknown.
func ContextWindow(ref string) int {
	if e, ok := Find(AvailableModels(), ref); ok {
		return e.ContextWindow
	}
	return 0
}

// Find matches a "provider/model", a bare model ID, or a unique model suffix.
func Find(models []ModelEntry, ref string) (ModelEntry, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ModelEntry{}, false
	}
	for _, e := range models {
		if e.Ref() == ref {
			return e, true
		}
	}
	var found ModelEntry
	n := 0
	for _, e := range models {
		if e.Model == ref || strings.HasSuffix(e.Ref(), "/"+ref) {
			found, n = e, n+1
		}
	}
	if n == 1 {
		return found, true
	}
	return ModelEntry{}, false
}

// ResolveKey resolves the entry's apiKey source, like Pi does: a literal,
// $NAME/${NAME} environment interpolation, or a leading !command.
func (e ModelEntry) ResolveKey() string {
	s := strings.TrimSpace(e.APIKey)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "!") {
		out, err := exec.Command("sh", "-c", s[1:]).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return expandEnv(s)
}

// ResolveHeaders resolves each header value like ResolveKey: a leading
// !command runs a shell command, other values get environment interpolation.
// A failed command is returned as a one-line error.
func (e ModelEntry) ResolveHeaders() (map[string]string, error) {
	if len(e.Headers) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(e.Headers))
	for k, v := range e.Headers {
		s := strings.TrimSpace(v)
		if strings.HasPrefix(s, "!") {
			cmd := s[1:]
			b, err := exec.Command("sh", "-c", cmd).Output()
			if err != nil {
				return nil, fmt.Errorf("header %q: command %q failed: %w", k, cmd, err)
			}
			out[k] = strings.TrimSpace(string(b))
			continue
		}
		out[k] = expandEnv(s)
	}
	return out, nil
}

func expandEnv(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) {
			switch s[i+1] {
			case '$':
				b.WriteByte('$')
				i += 2
				continue
			case '!':
				b.WriteByte('!')
				i += 2
				continue
			case '{':
				if j := strings.IndexByte(s[i+2:], '}'); j >= 0 {
					b.WriteString(os.Getenv(s[i+2 : i+2+j]))
					i += j + 3
					continue
				}
			default:
				j := i + 1
				for j < len(s) && (isNameByte(s[j])) {
					j++
				}
				if j > i+1 {
					b.WriteString(os.Getenv(s[i+1 : j]))
					i = j
					continue
				}
			}
		}
		b.WriteByte('$')
		i++
	}
	return b.String()
}

func isNameByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func mergeHeaders(base, over map[string]string) map[string]string {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

func firstRaw(a, b json.RawMessage) json.RawMessage {
	if len(a) > 0 {
		return a
	}
	return b
}
