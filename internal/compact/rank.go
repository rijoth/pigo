package compact

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// BriefRankingOptions tunes ranked brief selection.
type BriefRankingOptions struct {
	MaxBlocks            int
	PreserveRecentBlocks int
	FileOps              *FileOps
	MaxBriefChars        int
	MaxBriefCharsCeiling int
	BriefCharsPerBlock   int
}

var (
	editToolRe        = regexp.MustCompile(`(?i)^(edit|write|multiedit|quick_edit|target_edit|apply_patch)$`)
	readToolRe        = regexp.MustCompile(`(?i)^(read|glob|grep|ls|find|semantic_query|semantic_grep|semantic_show)$`)
	testCommandRe     = regexp.MustCompile(`(?i)\b(?:bun|npm|pnpm|yarn|node|pytest|cargo|go|mvn|gradle)\b[^\n]*(?:test|spec|check|lint|build|tsc)`)
	ghPrPollRe        = regexp.MustCompile(`(?:^|\s)gh\s+pr\s+(?:view|checks)\s+(\d+)\b`)
	workflowCommandRe = regexp.MustCompile(`(?i)(?:^|\s)(?:gh\s+(?:pr|issue)\s+[a-z-]+|git\s+(?:commit|push|merge|rebase|revert|cherry-pick|tag|reset|checkout|branch)\b)`)
	trivialBashLineRe = regexp.MustCompile(`^(?:set\s+[-+]|cd(?:\s+\S+)?$|export\s+\w+=|(?:source|\.)\s+\S+|pwd$|true$|:$|#|ls(?:\s|$)|echo\b|clear$|sleep\b)`)
	pathFromBashRe    = regexp.MustCompile(`(?:^|\s)([\w./-]+\.[\w-]+)(?:\s|$)`)
	normalizeCmdRe    = regexp.MustCompile(`\s+`)
)

const (
	defaultMaxBlocks         = 80
	defaultRecentBlocks      = 16
	minClosingAssistantChars = 120
	trivialBashPenalty       = 16
)

type rankedBlock struct {
	block   Block
	index   int
	score   int
	reasons []string
}

func (r *rankedBlock) add(points int, reason string) {
	r.score += points
	r.reasons = append(r.reasons, reason)
}

func isTrivialOnlyBash(raw string) bool {
	lines := strings.Split(raw, "\n")
	var kept []string
	for i := 0; i < len(lines); i++ {
		kept = append(kept, lines[i])
		if close := heredocCloseIndex(lines, i); close != -1 {
			i = close
		}
	}
	for _, l := range kept {
		l = strings.TrimSpace(l)
		if l == "" || trivialBashLineRe.MatchString(l) {
			continue
		}
		return false
	}
	return true
}

func asPathSet(paths []string) map[string]bool {
	set := map[string]bool{}
	for _, p := range paths {
		if p != "" {
			set[p] = true
		}
	}
	return set
}

func bashCommandFromBlock(b Block) string {
	if b.Kind == KindToolCall && strings.EqualFold(b.Name, "bash") {
		if cmd, ok := b.Args["command"].(string); ok {
			return cmd
		}
	}
	return ""
}

func pathFromBlock(b Block) string {
	if b.Kind == KindToolCall {
		return extractPath(b.Args)
	}
	if b.Kind == KindAssistant {
		return ""
	}
	return ""
}

func scoreBlock(b Block, index, total int, modifiedFiles, readFiles map[string]bool) rankedBlock {
	r := rankedBlock{block: b, index: index}
	recency := 0
	if total > 1 {
		recency = int(math.Round(float64(index) / float64(total-1) * 12))
	}
	r.add(recency, "recency")

	switch b.Kind {
	case KindUser:
		r.add(18, "user-turn")
	case KindAssistant:
		r.add(10, "assistant-context")
	case KindToolResult:
		r.add(1, "tool-result-low-value")
	case KindToolCall:
		command := bashCommandFromBlock(b)
		switch {
		case editToolRe.MatchString(b.Name):
			r.add(34, "edit-tool")
		case command != "" && testCommandRe.MatchString(command):
			r.add(26, "test-command")
		case readToolRe.MatchString(b.Name):
			r.add(6, "read-tool")
		default:
			r.add(12, "tool-call")
		}
		if command != "" && workflowCommandRe.MatchString(command) {
			r.add(14, "workflow-command")
		}
		if command != "" && isTrivialOnlyBash(command) {
			r.add(-trivialBashPenalty, "trivial-bash")
		}
	}

	if p := pathFromBlock(b); p != "" {
		if modifiedFiles[p] {
			r.add(18, "hook-modified-file")
		}
		if readFiles[p] {
			r.add(6, "hook-read-file")
		}
	}
	if b.Kind == KindToolResult && len(b.Text) > 1000 {
		r.add(-8, "long-tool-result")
	}
	return r
}

func boostAdjacency(ranked []rankedBlock) {
	var important []int
	for i := range ranked {
		r := ranked[i]
		if r.score >= 34 || contains(r.reasons, "edit-tool") || contains(r.reasons, "test-command") || contains(r.reasons, "nonzero-exit") {
			important = append(important, r.index)
		}
	}
	for _, idx := range important {
		for i := idx - 1; i >= 0 && i >= idx-8; i-- {
			if ranked[i].block.Kind == KindUser {
				ranked[i].add(10, "near-important-event")
				break
			}
		}
		for i := idx - 1; i >= 0 && i >= idx-4; i-- {
			if ranked[i].block.Kind == KindAssistant {
				ranked[i].add(7, "near-important-event")
				break
			}
		}
		for i := idx + 1; i < len(ranked) && i <= idx+4; i++ {
			if ranked[i].block.Kind == KindAssistant {
				ranked[i].add(5, "after-important-event")
				break
			}
		}
	}
}

func nextNonToolResult(ranked []rankedBlock, index int) (Block, bool) {
	for i := index + 1; i < len(ranked); i++ {
		if ranked[i].block.Kind != KindToolResult {
			return ranked[i].block, true
		}
	}
	return Block{}, false
}

func boostSegmentClosingAssistants(ranked []rankedBlock) {
	for i := range ranked {
		if ranked[i].block.Kind != KindAssistant {
			continue
		}
		if len([]rune(strings.TrimSpace(ranked[i].block.Text))) < minClosingAssistantChars {
			continue
		}
		next, ok := nextNonToolResult(ranked, i)
		if !ok || next.Kind == KindUser {
			ranked[i].add(14, "segment-closing-assistant")
		}
	}
}

func dedupKey(b Block) string {
	command := bashCommandFromBlock(b)
	if command != "" {
		if m := ghPrPollRe.FindStringSubmatch(command); m != nil {
			return "gh-pr-poll:" + m[1]
		}
		normalized := strings.TrimSpace(normalizeCmdRe.ReplaceAllString(command, " "))
		if normalized != "" {
			return "bash:" + normalized
		}
	}
	if b.Kind == KindToolCall {
		if p := pathFromBlock(b); p != "" {
			return "tool:" + strings.ToLower(b.Name) + ":" + p
		}
	}
	return ""
}

// RankBriefBlocks scores every block for brief selection.
func RankBriefBlocks(blocks []Block, options BriefRankingOptions) []rankedBlock {
	var modifiedFiles, readFiles map[string]bool
	if options.FileOps != nil {
		modifiedFiles = asPathSet(options.FileOps.Modified)
		readFiles = asPathSet(options.FileOps.Read)
	} else {
		modifiedFiles = map[string]bool{}
		readFiles = map[string]bool{}
	}
	ranked := make([]rankedBlock, len(blocks))
	for i, b := range blocks {
		ranked[i] = scoreBlock(b, i, len(blocks), modifiedFiles, readFiles)
	}
	boostAdjacency(ranked)
	boostSegmentClosingAssistants(ranked)
	return ranked
}

// SelectRankedBriefBlocks picks the highest-signal blocks under a size budget.
func SelectRankedBriefBlocks(blocks []Block, options BriefRankingOptions) []Block {
	maxBlocks := options.MaxBlocks
	if maxBlocks <= 0 {
		maxBlocks = defaultMaxBlocks
	}
	maxBriefChars := options.MaxBriefChars
	if options.MaxBriefChars > 0 && options.MaxBriefCharsCeiling > 0 && options.BriefCharsPerBlock > 0 {
		maxBriefChars = int(math.Round(math.Min(
			float64(options.MaxBriefCharsCeiling),
			math.Max(float64(options.MaxBriefChars), float64(options.BriefCharsPerBlock)*float64(len(blocks))),
		)))
	}
	if len(blocks) <= maxBlocks && maxBriefChars <= 0 {
		return blocks
	}

	preserve := options.PreserveRecentBlocks
	if preserve <= 0 {
		preserve = defaultRecentBlocks
	}
	if preserve > maxBlocks {
		preserve = maxBlocks
	}
	ranked := RankBriefBlocks(blocks, options)
	selected := map[int]bool{}
	seenKeys := map[string]bool{}

	var costs []int
	if maxBriefChars > 0 {
		costs = make([]int, len(blocks))
		for i, b := range blocks {
			if b.Kind == KindToolResult {
				costs[i] = 0
			} else {
				costs[i] = len(StringifyBrief(BuildBriefSections([]Block{b}))) + 1
			}
		}
	}
	usedChars := 0
	start := len(blocks) - preserve
	if start < 0 {
		start = 0
	}
	for i := len(blocks) - 1; i >= start; i-- {
		if blocks[i].Kind == KindToolResult || selected[i] {
			continue
		}
		if costs != nil && usedChars+costs[i] > maxBriefChars {
			continue
		}
		selected[i] = true
		if costs != nil {
			usedChars += costs[i]
		}
		if key := dedupKey(blocks[i]); key != "" {
			seenKeys[key] = true
		}
	}

	ordered := make([]rankedBlock, len(ranked))
	copy(ordered, ranked)
	sort.SliceStable(ordered, func(a, b int) bool {
		if ordered[a].score != ordered[b].score {
			return ordered[a].score > ordered[b].score
		}
		return ordered[a].index > ordered[b].index
	})
	for _, item := range ordered {
		if len(selected) >= maxBlocks {
			break
		}
		if selected[item.index] || item.block.Kind == KindToolResult {
			continue
		}
		if key := dedupKey(item.block); key != "" && seenKeys[key] {
			continue
		}
		if costs != nil {
			if usedChars+costs[item.index] > maxBriefChars {
				continue
			}
			usedChars += costs[item.index]
		}
		selected[item.index] = true
		if key := dedupKey(item.block); key != "" {
			seenKeys[key] = true
		}
	}

	idxs := make([]int, 0, len(selected))
	for i := range selected {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	out := make([]Block, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, blocks[i])
	}
	return out
}
