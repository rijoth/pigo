package fantasy

import (
	"testing"

	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openrouter"
)

func TestThinkingOptions(t *testing.T) {
	if opts := (&fantasyProvider{provider: "anthropic", reasoning: false}).thinkingOptions("high"); opts != nil {
		t.Fatalf("non-reasoning model got options: %+v", opts)
	}
	if opts := (&fantasyProvider{provider: "anthropic", reasoning: true}).thinkingOptions("off"); opts != nil {
		t.Fatalf("off got anthropic options: %+v", opts)
	}

	an := &fantasyProvider{provider: "anthropic", reasoning: true}
	po, ok := an.thinkingOptions("medium")[anthropic.Name].(*anthropic.ProviderOptions)
	if !ok || po.Effort == nil || *po.Effort != anthropic.EffortMedium {
		t.Fatalf("anthropic medium = %+v", po)
	}

	oa := &fantasyProvider{provider: "openai", reasoning: true}
	oo, ok := oa.thinkingOptions("high")[openai.Name].(*openai.ProviderOptions)
	if !ok || oo.ReasoningEffort == nil || *oo.ReasoningEffort != openai.ReasoningEffortHigh {
		t.Fatalf("openai high = %+v", oo)
	}
	offOpts, ok := oa.thinkingOptions("off")[openai.Name].(*openai.ProviderOptions)
	if !ok || offOpts.ReasoningEffort == nil || *offOpts.ReasoningEffort != openai.ReasoningEffortNone {
		t.Fatalf("openai off = %+v", offOpts)
	}

	or := &fantasyProvider{provider: "openrouter", reasoning: true}
	ro, ok := or.thinkingOptions("low")[openrouter.Name].(*openrouter.ProviderOptions)
	if !ok || ro.Reasoning == nil || ro.Reasoning.Effort == nil || *ro.Reasoning.Effort != openrouter.ReasoningEffortLow {
		t.Fatalf("openrouter low = %+v", ro)
	}

	goo := &fantasyProvider{provider: "google", reasoning: true}
	gg, ok := goo.thinkingOptions("minimal")[google.Name].(*google.ProviderOptions)
	if !ok || gg.ThinkingConfig == nil || gg.ThinkingConfig.ThinkingLevel == nil || *gg.ThinkingConfig.ThinkingLevel != "MINIMAL" {
		t.Fatalf("google minimal = %+v", gg)
	}

	mapped := "low"
	over := &fantasyProvider{provider: "anthropic", reasoning: true, levelMap: map[string]*string{"medium": &mapped}}
	mo, _ := over.thinkingOptions("medium")[anthropic.Name].(*anthropic.ProviderOptions)
	if mo == nil || mo.Effort == nil || *mo.Effort != anthropic.EffortLow {
		t.Fatalf("mapped anthropic medium = %+v", mo)
	}

	// A null mapping means "no explicit provider value": no options, so the
	// provider default applies (this is how opencode-go maps medium).
	null := &fantasyProvider{provider: "openai", reasoning: true, levelMap: map[string]*string{"medium": nil, "off": nil}}
	if opts := null.thinkingOptions("medium"); opts != nil {
		t.Fatalf("null medium sent options: %+v", opts)
	}
	// off still tries to disable even when the map has no explicit value.
	offOff, ok := null.thinkingOptions("off")[openai.Name].(*openai.ProviderOptions)
	if !ok || offOff.ReasoningEffort == nil || *offOff.ReasoningEffort != openai.ReasoningEffortNone {
		t.Fatalf("null off = %+v", offOff)
	}
}
