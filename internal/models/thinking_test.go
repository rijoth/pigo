package models

import (
	"strings"
	"testing"
)

func TestSupportedThinkingLevels(t *testing.T) {
	xhigh := "xhigh"
	withXHigh := ModelEntry{Reasoning: true, ThinkingLevelMap: map[string]*string{"xhigh": &xhigh}}
	// The opencode-go/deepseek-v4.1-flash map: null standard levels stay
	// selectable, xhigh stays out because it has no explicit value.
	opencode := ModelEntry{Reasoning: true, ThinkingLevelMap: map[string]*string{
		"off": nil, "minimal": nil, "low": strPtr("low"), "medium": nil,
		"high": strPtr("high"), "xhigh": nil, "max": strPtr("max"),
	}}

	cases := []struct {
		name  string
		entry ModelEntry
		want  string
	}{
		{"no reasoning", ModelEntry{}, "off"},
		{"default", ModelEntry{Reasoning: true}, "off,minimal,low,medium,high"},
		{"null standard levels", opencode, "off,minimal,low,medium,high,max"},
		{"xhigh mapped", withXHigh, "off,minimal,low,medium,high,xhigh"},
	}
	for _, tc := range cases {
		got := strings.Join(tc.entry.SupportedThinkingLevels(), ",")
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestClampThinkingLevel(t *testing.T) {
	entry := ModelEntry{Reasoning: true}
	if got := entry.ClampThinkingLevel("max"); got != "high" {
		t.Errorf("max clamped to %q, want high", got)
	}
	if got := entry.ClampThinkingLevel("minimal"); got != "minimal" {
		t.Errorf("minimal clamped to %q", got)
	}
	if got := entry.ClampThinkingLevel("bogus"); got != "off" {
		t.Errorf("bogus clamped to %q, want off", got)
	}
	if got := (ModelEntry{}).ClampThinkingLevel("high"); got != "off" {
		t.Errorf("non-reasoning clamped to %q, want off", got)
	}
}

func TestNextThinkingLevel(t *testing.T) {
	levels := []string{"off", "low", "high"}
	if got := NextThinkingLevel(levels, "high"); got != "off" {
		t.Errorf("wrap = %q, want off", got)
	}
	if got := NextThinkingLevel(levels, "off"); got != "low" {
		t.Errorf("next = %q, want low", got)
	}
	if got := NextThinkingLevel(levels, "missing"); got != "off" {
		t.Errorf("unknown = %q, want off", got)
	}
}
