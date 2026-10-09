package providers

import (
	"os"
	"pigo/internal/auth"
	"pigo/internal/models"
	"sort"
)

// ProviderInfo describes a provider shown by /login.
type ProviderInfo struct {
	ID     string
	Name   string
	EnvVar string // standard API-key variable, "" when none is known
	OAuth  bool   // only OAuth/subscription login; unsupported in v1
}

// builtinProviders are always offered, independently of models.json. Most are
// the providers behind BuiltinModels; openai-codex is listed only so /login can
// say it needs an API key rather than silently omitting it.
var builtinProviders = []ProviderInfo{
	{ID: "anthropic", Name: "Anthropic", EnvVar: "ANTHROPIC_API_KEY"},
	{ID: "openai", Name: "OpenAI", EnvVar: "OPENAI_API_KEY"},
	{ID: "google", Name: "Google Gemini", EnvVar: "GEMINI_API_KEY"},
	{ID: "openrouter", Name: "OpenRouter", EnvVar: "OPENROUTER_API_KEY"},
	{ID: "opencode", Name: "OpenCode", EnvVar: "OPENCODE_API_KEY"},
	{ID: "opencode-go", Name: "OpenCode Go", EnvVar: "OPENCODE_API_KEY"},
	{ID: "openai-codex", Name: "OpenAI Codex (legacy)", OAuth: true},
}

// providerEnv maps a provider id to its standard API-key variable, mirroring
// Pi's env-api-keys table. It gives models.json providers a key source even
// when the entry itself lists none.
var providerEnv = map[string]string{
	"ant-ling":                   "ANT_LING_API_KEY",
	"qwen-token-plan":            "QWEN_TOKEN_PLAN_API_KEY",
	"qwen-token-plan-cn":         "QWEN_TOKEN_PLAN_CN_API_KEY",
	"qwen-token-plan-individual": "QWEN_TOKEN_PLAN_API_KEY",
	"openai":                     "OPENAI_API_KEY",
	"azure-openai-responses":     "AZURE_OPENAI_API_KEY",
	"nvidia":                     "NVIDIA_API_KEY",
	"deepseek":                   "DEEPSEEK_API_KEY",
	"google":                     "GEMINI_API_KEY",
	"google-vertex":              "GOOGLE_CLOUD_API_KEY",
	"groq":                       "GROQ_API_KEY",
	"cerebras":                   "CEREBRAS_API_KEY",
	"xai":                        "XAI_API_KEY",
	"typesafe":                   "TYPESAFE_API_KEY",
	"radius":                     "RADIUS_API_KEY",
	"openrouter":                 "OPENROUTER_API_KEY",
	"vercel-ai-gateway":          "AI_GATEWAY_API_KEY",
	"zai":                        "ZAI_API_KEY",
	"zai-coding-cn":              "ZAI_CODING_CN_API_KEY",
	"mistral":                    "MISTRAL_API_KEY",
	"minimax":                    "MINIMAX_API_KEY",
	"minimax-cn":                 "MINIMAX_CN_API_KEY",
	"moonshotai":                 "MOONSHOT_API_KEY",
	"moonshotai-cn":              "MOONSHOT_API_KEY",
	"huggingface":                "HF_TOKEN",
	"fireworks":                  "FIREWORKS_API_KEY",
	"together":                   "TOGETHER_API_KEY",
	"baseten":                    "BASETEN_API_KEY",
	"opencode":                   "OPENCODE_API_KEY",
	"opencode-go":                "OPENCODE_API_KEY",
	"kimi-coding":                "KIMI_API_KEY",
	"meta":                       "META_API_KEY",
	"cloudflare-workers-ai":      "CLOUDFLARE_API_KEY",
	"cloudflare-ai-gateway":      "CLOUDFLARE_API_KEY",
	"xiaomi":                     "XIAOMI_API_KEY",
	"xiaomi-token-plan-cn":       "XIAOMI_TOKEN_PLAN_CN_API_KEY",
	"xiaomi-token-plan-ams":      "XIAOMI_TOKEN_PLAN_AMS_API_KEY",
	"xiaomi-token-plan-sgp":      "XIAOMI_TOKEN_PLAN_SGP_API_KEY",
}

// EnvVars returns the standard API-key variables for provider, most canonical
// first. Google accepts the historical GOOGLE_API_KEY as a fallback.
func EnvVars(provider string) []string {
	switch provider {
	case "anthropic":
		return []string{"ANTHROPIC_API_KEY"}
	case "github-copilot":
		return []string{"COPILOT_GITHUB_TOKEN"}
	case "google", "gemini":
		return []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}
	}
	if v, ok := providerEnv[provider]; ok {
		return []string{v}
	}
	return nil
}

// EnvVar returns the primary standard variable for provider, or "".
func EnvVar(provider string) string {
	vs := EnvVars(provider)
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

// ResolveKey returns the API key for provider in pigo's fixed order: auth.json,
// then the provider's standard env var(s), then the models.json entry's apiKey
// source. A models.json entry is optional for built-in providers; pass the
// zero value when there is none.
func ResolveKey(provider string, e models.ModelEntry, authPath string) (string, error) {
	key, err := auth.Key(authPath, provider)
	if err != nil {
		return "", err
	}
	if key != "" {
		return key, nil
	}
	for _, name := range EnvVars(provider) {
		if v := os.Getenv(name); v != "" {
			return v, nil
		}
	}
	return e.ResolveKey(), nil
}

// AuthenticatedModels returns entries whose provider has a usable API key,
// resolving each provider once so a models.json apiKey is not lost when the
// catalog also lists the provider. Pi's model picker lists only these.
func AuthenticatedModels(entries []models.ModelEntry, authPath string) []models.ModelEntry {
	reps := make(map[string]models.ModelEntry)
	for _, e := range entries {
		r, ok := reps[e.Provider]
		if !ok || (r.APIKey == "" && e.APIKey != "") {
			reps[e.Provider] = e
		}
	}
	authed := make(map[string]bool, len(reps))
	for id, rep := range reps {
		key, err := ResolveKey(id, rep, authPath)
		authed[id] = err == nil && key != ""
	}
	out := make([]models.ModelEntry, 0, len(entries))
	for _, e := range entries {
		if authed[e.Provider] {
			out = append(out, e)
		}
	}
	return out
}

// Providers returns the built-in providers merged with those present in
// models.json, sorted by id.
func Providers() []ProviderInfo {
	byID := make(map[string]ProviderInfo)
	for _, p := range builtinProviders {
		byID[p.ID] = p
	}
	for _, e := range models.AvailableModels() {
		if _, ok := byID[e.Provider]; ok {
			continue
		}
		byID[e.Provider] = ProviderInfo{ID: e.Provider, Name: e.Provider, EnvVar: EnvVar(e.Provider)}
	}
	out := make([]ProviderInfo, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FindProvider looks up a provider id in Providers.
func FindProvider(id string) (ProviderInfo, bool) {
	for _, p := range Providers() {
		if p.ID == id {
			return p, true
		}
	}
	return ProviderInfo{}, false
}
