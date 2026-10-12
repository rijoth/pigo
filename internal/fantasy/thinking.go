package fantasy

import (
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"pigo/internal/models"
)

// normalizeProvider folds provider aliases into the fantasy provider name.
func normalizeProvider(name string) string {
	if name == "gemini" {
		return "google"
	}
	return name
}

// entryProvider reports the fantasy provider a models.json entry maps to,
// matching the choice made by fantasyForEntry.
func entryProvider(e models.ModelEntry) string {
	switch {
	case e.API == "anthropic-messages":
		return "anthropic"
	case e.API == "openai-responses":
		return "openai"
	case e.API == "google-generative-ai":
		return "google"
	case e.API == "openai-completions" || e.BaseURL != "":
		return "openai-compat"
	default:
		return normalizeProvider(e.Provider)
	}
}

// thinkingOptions maps a reasoning level to provider-specific options. It
// returns nil when reasoning is unsupported, the level is a no-op, or the
// model maps it to null (no explicit provider value), so the request keeps
// the provider's default.
func (f *fantasyProvider) thinkingOptions(level string) fantasy.ProviderOptions {
	if !f.reasoning || level == "" {
		return nil
	}
	native := level
	switch {
	case level == "off":
		// Disabling is provider-specific: a mapped value wins, otherwise
		// OpenAI-style providers get "none".
		if v, ok := f.levelMap["off"]; ok && v != nil {
			native = *v
		} else {
			native = "none"
		}
	default:
		if v, ok := f.levelMap[level]; ok {
			if v == nil {
				return nil // no explicit value: leave the provider default
			}
			native = *v
		}
	}
	switch f.provider {
	case "anthropic":
		if level == "off" {
			return nil
		}
		effort := anthropicEffort(native)
		return anthropic.NewProviderOptions(&anthropic.ProviderOptions{Effort: &effort})
	case "openai":
		e := openai.ReasoningEffort(native)
		return openai.NewProviderOptions(&openai.ProviderOptions{ReasoningEffort: &e})
	case "openrouter":
		e := openrouter.ReasoningEffort(native)
		return openrouter.NewProviderOptions(&openrouter.ProviderOptions{Reasoning: &openrouter.ReasoningOptions{Effort: &e}})
	case "google":
		if level == "off" {
			return nil
		}
		lvl := strings.ToUpper(native)
		return fantasy.ProviderOptions{google.Name: &google.ProviderOptions{ThinkingConfig: &google.ThinkingConfig{ThinkingLevel: &lvl}}}
	default:
		e := openai.ReasoningEffort(native)
		return openaicompat.NewProviderOptions(&openaicompat.ProviderOptions{ReasoningEffort: &e})
	}
}

// anthropicEffort maps a thinking level to an Anthropic output effort. Setting
// effort also turns on adaptive thinking in the Fantasy adapter.
func anthropicEffort(native string) anthropic.Effort {
	switch native {
	case "minimal", "low":
		return anthropic.EffortLow
	case "medium":
		return anthropic.EffortMedium
	case "high":
		return anthropic.EffortHigh
	case "xhigh", "max":
		return anthropic.EffortMax
	default:
		return anthropic.EffortHigh
	}
}
