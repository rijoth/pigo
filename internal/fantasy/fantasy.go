package fantasy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"

	"pigo/internal/auth"
	"pigo/internal/llm"
	"pigo/internal/models"
	"pigo/internal/providers"
)

type fantasyProvider struct {
	model     fantasy.LanguageModel
	provider  string            // fantasy provider name: anthropic, openai, openrouter, google, openai-compat
	levelMap  map[string]string // thinkingLevelMap overrides from models.json
	reasoning bool              // model declares reasoning support
}

// Version is the pigo version advertised in the User-Agent header.
var Version = "0.1.0"

// attributionTransport stamps configured headers on every request and, for
// OpenCode providers, the current session id and pigo User-Agent. Configured
// headers win case-insensitively over the generated ones.
type attributionTransport struct {
	base     http.RoundTripper
	headers  map[string]string
	has      map[string]bool
	opencode bool
	session  func() string
}

func (t *attributionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if t.opencode {
		if !t.has["x-opencode-session"] && t.session != nil {
			if id := t.session(); id != "" {
				req.Header.Set("x-opencode-session", id)
			}
		}
		if !t.has["user-agent"] {
			req.Header.Set("User-Agent", "pigo/"+Version)
		}
	}
	return t.base.RoundTrip(req)
}

// attributionClient returns a client that sends the configured headers and, for
// provider ids starting with "opencode", the session attribution headers.
func attributionClient(provider string, headers map[string]string, session func() string) *http.Client {
	has := make(map[string]bool, len(headers))
	for k := range headers {
		has[strings.ToLower(k)] = true
	}
	return &http.Client{Transport: &attributionTransport{
		base:     http.DefaultTransport,
		headers:  headers,
		has:      has,
		opencode: strings.HasPrefix(provider, "opencode"),
		session:  session,
	}}
}

// NewFantasy builds a provider from its name and resolves a model. API keys
// come from each provider's standard environment variable. session returns the
// current session id, used to attribute OpenCode requests.
func NewFantasy(providerName, modelID string, session func() string) (llm.Provider, error) {
	provider, err := newFantasyProvider(providerName, session)
	if err != nil {
		return nil, err
	}
	model, err := provider.LanguageModel(context.Background(), modelID)
	if err != nil {
		return nil, fmt.Errorf("model %q: %w", modelID, err)
	}
	return &fantasyProvider{model: model, provider: normalizeProvider(providerName)}, nil
}

// NewFantasyEntry builds a provider for a models.json entry, using a custom
// base URL with OpenAI-compatible mode when the entry specifies one.
func NewFantasyEntry(e models.ModelEntry, session func() string) (llm.Provider, error) {
	p, err := fantasyForEntry(e, session)
	if err != nil {
		return nil, err
	}
	model, err := p.LanguageModel(context.Background(), e.Model)
	if err != nil {
		return nil, fmt.Errorf("model %q: %w", e.Model, err)
	}
	return &fantasyProvider{model: model, provider: entryProvider(e), levelMap: levelOverrides(e.ThinkingLevelMap), reasoning: e.Reasoning}, nil
}

// NewFromRef builds a provider from a models.json reference. found is false
// when no entry matches.
func NewFromRef(ref string, session func() string) (llm.Provider, string, bool, error) {
	e, ok := models.Find(models.AvailableModels(), ref)
	if !ok {
		return nil, "", false, nil
	}
	p, err := NewFantasyEntry(e, session)
	if err != nil {
		return nil, "", true, err
	}
	return p, e.Ref(), true, nil
}

// fantasyForEntry picks the Fantasy provider from the entry's api field. A
// custom base URL defaults to an OpenAI-compatible endpoint.
func fantasyForEntry(e models.ModelEntry, session func() string) (fantasy.Provider, error) {
	api := e.API
	if api == "" && e.BaseURL != "" {
		api = "openai-completions"
	}
	headers, err := e.ResolveHeaders()
	if err != nil {
		return nil, err
	}
	key, err := providers.ResolveKey(e.Provider, e, auth.Path())
	if err != nil {
		return nil, err
	}
	client := attributionClient(e.Provider, headers, session)
	switch {
	case api == "openai-completions":
		opts := []openaicompat.Option{openaicompat.WithHTTPClient(client)}
		if e.BaseURL != "" {
			opts = append(opts, openaicompat.WithBaseURL(e.BaseURL))
		}
		if key != "" {
			opts = append(opts, openaicompat.WithAPIKey(key))
		}
		return openaicompat.New(opts...)
	case api == "openai-responses":
		opts := []openai.Option{openai.WithUseResponsesAPI(), openai.WithHTTPClient(client)}
		if e.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(e.BaseURL))
		}
		if key != "" {
			opts = append(opts, openai.WithAPIKey(key))
		}
		return openai.New(opts...)
	case api == "anthropic-messages":
		opts := []anthropic.Option{anthropic.WithHTTPClient(client)}
		if e.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(e.BaseURL))
		}
		if key != "" {
			opts = append(opts, anthropic.WithAPIKey(key))
		}
		return anthropic.New(opts...)
	case api == "google-generative-ai":
		opts := []google.Option{google.WithHTTPClient(client)}
		if e.BaseURL != "" {
			opts = append(opts, google.WithBaseURL(e.BaseURL))
		}
		if key != "" {
			opts = append(opts, google.WithGeminiAPIKey(key))
		}
		return google.New(opts...)
	case e.BaseURL != "":
		// Unknown api with a custom endpoint: assume OpenAI-compatible.
		opts := []openaicompat.Option{openaicompat.WithBaseURL(e.BaseURL), openaicompat.WithHTTPClient(client)}
		if key != "" {
			opts = append(opts, openaicompat.WithAPIKey(key))
		}
		return openaicompat.New(opts...)
	}
	return newFantasyProvider(e.Provider, session)
}

// ResolveModel returns the provider and model from a flag of the form
// "provider/model" or "model", falling back to PIGO_PROVIDER and PIGO_MODEL.
func ResolveModel(flagVal, defaultProvider string) (string, string, error) {
	provider := os.Getenv("PIGO_PROVIDER")
	model := os.Getenv("PIGO_MODEL")
	if flagVal != "" {
		if p, m, ok := strings.Cut(flagVal, "/"); ok {
			provider, model = p, m
		} else {
			model = flagVal
		}
	}
	if provider == "" {
		provider = defaultProvider
	}
	if model == "" {
		return "", "", fmt.Errorf("no model set")
	}
	return provider, model, nil
}

// BuildProvider builds a provider from a flag. When no model is configured it
// returns a dummy provider and the label "none" so the UI can still run.
func BuildProvider(flagVal, defaultProvider string, session func() string) (llm.Provider, string, error) {
	if flagVal != "" {
		p, label, found, err := NewFromRef(flagVal, session)
		if err != nil {
			return nil, "", err
		}
		if found {
			return p, label, nil
		}
	}
	name, model, err := ResolveModel(flagVal, defaultProvider)
	if err != nil {
		return llm.NewDummy(), "none", nil
	}
	p, err := NewFantasy(name, model, session)
	if err != nil {
		return nil, "", err
	}
	return p, name + "/" + model, nil
}

func newFantasyProvider(name string, session func() string) (fantasy.Provider, error) {
	key, err := providers.ResolveKey(name, models.ModelEntry{}, auth.Path())
	if err != nil {
		return nil, err
	}
	client := attributionClient(name, nil, session)
	switch name {
	case "anthropic":
		return anthropic.New(anthropic.WithAPIKey(key), anthropic.WithHTTPClient(client))
	case "openai":
		return openai.New(openai.WithAPIKey(key), openai.WithHTTPClient(client))
	case "openrouter":
		return openrouter.New(openrouter.WithAPIKey(key), openrouter.WithHTTPClient(client))
	case "google", "gemini":
		return google.New(google.WithGeminiAPIKey(key), google.WithHTTPClient(client))
	default:
		return nil, fmt.Errorf("unknown provider %q", name)
	}
}

func (f *fantasyProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	seq, err := f.model.Stream(ctx, fantasy.Call{
		Prompt:          toFantasyPrompt(req),
		Tools:           toFantasyTools(req.Tools),
		ProviderOptions: f.thinkingOptions(req.Thinking),
	})
	if err != nil {
		return nil, err
	}
	ch := make(chan llm.Delta)
	go func() {
		defer close(ch)
		// Reasoning arrives as a start/delta/end sequence; text and the
		// signature (provider metadata) may arrive in separate parts, so
		// gather one block and emit it complete.
		active := &llm.ReasoningBlock{}
		for part := range seq {
			switch part.Type {
			case fantasy.StreamPartTypeReasoningStart, fantasy.StreamPartTypeReasoningDelta:
				active.Text += part.Delta
				if len(part.ProviderMetadata) > 0 {
					active.Metadata = marshalMetadata(part.ProviderMetadata)
				}
				continue
			case fantasy.StreamPartTypeReasoningEnd:
				b := active
				active = &llm.ReasoningBlock{}
				if len(part.ProviderMetadata) > 0 {
					b.Metadata = marshalMetadata(part.ProviderMetadata)
				}
				if b.Text == "" && len(b.Metadata) == 0 {
					continue
				}
				select {
				case ch <- llm.Delta{Reasoning: b}:
				case <-ctx.Done():
					return
				}
				continue
			}
			var d llm.Delta
			switch part.Type {
			case fantasy.StreamPartTypeTextDelta:
				d.Text = part.Delta
			case fantasy.StreamPartTypeToolCall:
				input := part.ToolCallInput
				if input == "" {
					input = "{}"
				}
				d.Call = &llm.ToolCall{ID: part.ID, Name: part.ToolCallName, Args: json.RawMessage(input)}
			case fantasy.StreamPartTypeFinish:
				d.Usage = &llm.Usage{
					InputTokens:  int(part.Usage.InputTokens),
					OutputTokens: int(part.Usage.OutputTokens),
				}
			case fantasy.StreamPartTypeError:
				d.Err = part.Error
			default:
				continue
			}
			select {
			case ch <- d:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// marshalMetadata serializes provider metadata (reasoning signatures) to
// opaque JSON so it can be replayed later. It returns nil on failure.
func marshalMetadata(m fantasy.ProviderMetadata) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}

// providerOptions rebuilds provider options from stored metadata JSON. An
// unknown provider type is dropped rather than failing the whole request.
func providerOptions(raw json.RawMessage) fantasy.ProviderOptions {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	opts, err := fantasy.UnmarshalProviderMetadata(m)
	if err != nil {
		return nil
	}
	return fantasy.ProviderOptions(opts)
}

func toFantasyPrompt(req llm.Request) fantasy.Prompt {
	var prompt fantasy.Prompt
	if req.System != "" {
		prompt = append(prompt, fantasy.NewSystemMessage(req.System))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case llm.RoleUser:
			prompt = append(prompt, fantasy.NewUserMessage(m.Text))
		case llm.RoleAssistant:
			parts := make([]fantasy.MessagePart, 0, len(m.Reasoning)+len(m.Calls)+1)
			for _, r := range m.Reasoning {
				if r.Text == "" && len(r.Metadata) == 0 {
					continue
				}
				parts = append(parts, fantasy.ReasoningPart{Text: r.Text, ProviderOptions: providerOptions(r.Metadata)})
			}
			if m.Text != "" {
				parts = append(parts, fantasy.TextPart{Text: m.Text})
			}
			for _, c := range m.Calls {
				parts = append(parts, fantasy.ToolCallPart{
					ToolCallID: c.ID,
					ToolName:   c.Name,
					Input:      string(c.Args),
				})
			}
			prompt = append(prompt, fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts})
		case llm.RoleTool:
			parts := make([]fantasy.MessagePart, 0, len(m.Results))
			for _, r := range m.Results {
				var out fantasy.ToolResultOutputContent = fantasy.ToolResultOutputContentText{Text: r.Content}
				if r.IsError {
					out = fantasy.ToolResultOutputContentError{Error: errors.New(r.Content)}
				}
				parts = append(parts, fantasy.ToolResultPart{ToolCallID: r.CallID, Output: out})
			}
			prompt = append(prompt, fantasy.Message{Role: fantasy.MessageRoleTool, Content: parts})
		}
	}
	return prompt
}

func toFantasyTools(specs []llm.ToolSpec) []fantasy.Tool {
	tools := make([]fantasy.Tool, 0, len(specs))
	for _, s := range specs {
		var schema map[string]any
		if len(s.Schema) > 0 {
			_ = json.Unmarshal(s.Schema, &schema)
		}
		tools = append(tools, fantasy.FunctionTool{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: schema,
		})
	}
	return tools
}
