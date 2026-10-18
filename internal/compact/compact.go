package compact

import (
	"fmt"
	"strings"

	"pigo/internal/llm"
)

// SummaryPrefix is prepended to the stored summary message. It marks a
// synthetic user turn so it is never re-summarized as a user goal.
const SummaryPrefix = "[Earlier conversation summary]\n"

// BranchSummaryPrefix is prepended to a /tree branch summary message.
const BranchSummaryPrefix = "[Branch summary]\n"

// Stats describes one compaction for the UI notice.
type Stats struct {
	Summarized    int
	Kept          int
	KeptTurns     int
	TotalTurns    int
	KeptTokensEst int
	BudgetCut     string
	SmartAdjusted bool
	FromKeep      int
}

// Result is the outcome of Compact.
type Result struct {
	Summary    string
	Prompt     string
	FirstKept  int // index into the original history of the first kept message
	CompactAll bool
	Cut        Cut
	Stats      Stats
	OK         bool
}

// FormatStats renders a pi-vcc style compaction notice.
func FormatStats(s Stats) string {
	formatTokens := func(n int) string {
		if n >= 1000 {
			return fmt.Sprintf("%.1fk", float64(n)/1000)
		}
		return itoa(n)
	}
	if s.BudgetCut != "" {
		reason := "no user anchor"
		if s.BudgetCut == "oversized_tail" {
			reason = "oversized tail"
		}
		return fmt.Sprintf("kept ~%s tok tail (mid-turn cut, %s), summarized %d.", formatTokens(s.KeptTokensEst), reason, s.Summarized)
	}
	notes := []string{fmt.Sprintf("summarized %d", s.Summarized)}
	if s.SmartAdjusted {
		notes = append(notes, "smart-keep")
	}
	return fmt.Sprintf("kept %d/%d turns, ~%s tok (%s).", s.KeptTurns, s.TotalTurns, formatTokens(s.KeptTokensEst), strings.Join(notes, ", "))
}

func isSummaryMessage(m llm.Message) bool {
	return m.Role == llm.RoleUser && strings.HasPrefix(m.Text, SummaryPrefix)
}

// Compact deterministically summarizes older history and returns the new
// history plus the summary. ids is parallel to history ("" marks a synthetic
// summary). prompt, when non-empty, is a follow-up prompt returned in Result.
// globalIndex maps a record id to its session-global index for #N refs; a nil
// map omits refs (fail-closed).
func Compact(history []llm.Message, ids []string, keep int, explicit bool, prompt string, globalIndex func(string) int, opts Options) ([]llm.Message, Result, error) {
	live := history
	liveIDs := ids
	offset := 0
	prev := ""
	if len(history) > 0 && isSummaryMessage(history[0]) {
		prev = strings.TrimPrefix(history[0].Text, SummaryPrefix)
		live = history[1:]
		if len(ids) > 0 {
			liveIDs = ids[1:]
		} else {
			liveIDs = nil
		}
		offset = 1
	}

	o := opts
	o.Keep = keep
	o.Explicit = explicit
	o = o.withDefaults()

	cut, ok := ResolveCut(live, o)
	if !ok {
		return history, Result{Prompt: prompt}, nil
	}

	src := make([]int, len(live))
	for i := range live {
		src[i] = -1
		if globalIndex != nil && i < len(liveIDs) && liveIDs[i] != "" {
			src[i] = globalIndex(liveIDs[i])
		}
	}

	summary := CompileRanked(Input{
		History:       live,
		Cut:           cut.FirstKept,
		Previous:      prev,
		SourceIndices: src,
		TrackCommands: o.TrackCommands,
	}, o)
	if summary == "" {
		return history, Result{Prompt: prompt}, fmt.Errorf("compaction produced an empty summary")
	}

	cutIdx := cut.FirstKept
	if cutIdx > len(live) {
		cutIdx = len(live)
	}
	newHistory := append([]llm.Message{{Role: llm.RoleUser, Text: SummaryPrefix + summary}}, live[cutIdx:]...)

	stats := Stats{
		Summarized:    cutIdx,
		Kept:          len(live) - cutIdx,
		KeptTurns:     cut.KeptTurns,
		TotalTurns:    cut.TotalTurns,
		KeptTokensEst: EstimateTokens(live[cutIdx:], o.CharsPerToken),
		BudgetCut:     cut.BudgetCut,
		SmartAdjusted: cut.SmartAdjusted,
		FromKeep:      cut.FromKeep,
	}
	return newHistory, Result{
		Summary:    summary,
		Prompt:     prompt,
		FirstKept:  offset + cutIdx,
		CompactAll: cut.CompactAll,
		Cut:        cut,
		Stats:      stats,
		OK:         true,
	}, nil
}
