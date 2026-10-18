package compact

import "pigo/internal/llm"

// Cut describes the resolved compaction boundary.
type Cut struct {
	FirstKept     int  // index into the live window of the first kept message
	CompactAll    bool // summarize everything; keep no tail
	KeptTurns     int
	TotalTurns    int
	BudgetCut     string // "", "no_anchor", or "oversized_tail"
	Fallback      bool   // compact-all came from a keep fallback, not keep:0
	SmartAdjusted bool
	FromKeep      int
}

// ownCut is the internal cut result before it is projected onto Cut.
type ownCut struct {
	ok         bool
	firstKept  int
	compactAll bool
	keptTurns  int
	totalTurns int
	fallback   bool
	budgetCut  string
}

func prefixHasRenderable(live []llm.Message) bool {
	if len(live) == 0 {
		return false
	}
	return len(FilterNoise(Normalize(live, nil))) > 0
}

func userTurns(live []llm.Message) []int {
	var idx []int
	for i, m := range live {
		if m.Role == llm.RoleUser {
			idx = append(idx, i)
		}
	}
	return idx
}

func buildOwnCut(live []llm.Message, keep int) ownCut {
	if len(live) == 0 || len(live) <= 2 {
		return ownCut{}
	}
	if !prefixHasRenderable(live) {
		return ownCut{}
	}
	users := userTurns(live)
	compactAll := func(fallback bool) ownCut {
		return ownCut{ok: true, firstKept: len(live), compactAll: true, totalTurns: len(users), fallback: fallback}
	}
	if keep <= 0 {
		return compactAll(false)
	}
	target := len(users) - keep
	cutIdx := -1
	if target >= 0 {
		cutIdx = users[target]
	}
	if cutIdx <= 0 {
		return compactAll(true)
	}
	if !prefixHasRenderable(live[:cutIdx]) {
		return compactAll(true)
	}
	return ownCut{ok: true, firstKept: cutIdx, keptTurns: len(users) - target, totalTurns: len(users)}
}

func findBudgetCutIndex(live []llm.Message, maxTokens int, charsPerToken float64) int {
	acc := 0
	crossed := -1
	for i := len(live) - 1; i >= 0; i-- {
		acc += EstimateTokensFromChars(EstimateMessageChars(live[i]), charsPerToken)
		if acc >= maxTokens {
			crossed = i
			break
		}
	}
	if crossed < 0 {
		return -1
	}
	start := crossed
	if start < 1 {
		start = 1
	}
	for j := start; j < len(live); j++ {
		if live[j].Role != llm.RoleTool {
			return j
		}
	}
	return -1
}

func (o ownCut) budgetResult(live []llm.Message, idx int, kind string) ownCut {
	keptTurns := 0
	for _, m := range live[idx:] {
		if m.Role == llm.RoleUser {
			keptTurns++
		}
	}
	total := 0
	for _, m := range live {
		if m.Role == llm.RoleUser {
			total++
		}
	}
	return ownCut{ok: true, firstKept: idx, totalTurns: total, keptTurns: keptTurns, budgetCut: kind}
}

func applyTailBudget(live []llm.Message, cut ownCut, maxTokens int, factor, charsPerToken float64) ownCut {
	if !cut.ok {
		return cut
	}
	renders := func(idx int) bool { return prefixHasRenderable(live[:idx]) }

	if cut.compactAll {
		if !cut.fallback {
			return cut
		}
		idx := findBudgetCutIndex(live, maxTokens, charsPerToken)
		if idx < 0 || !renders(idx) {
			return cut
		}
		return cut.budgetResult(live, idx, "no_anchor")
	}

	tailStart := cut.firstKept
	tailTokens := 0
	for i := tailStart; i < len(live); i++ {
		tailTokens += EstimateTokensFromChars(EstimateMessageChars(live[i]), charsPerToken)
	}
	if float64(tailTokens) <= float64(maxTokens)*factor {
		return cut
	}
	idx := findBudgetCutIndex(live, maxTokens, charsPerToken)
	if idx <= tailStart {
		return cut
	}
	return cut.budgetResult(live, idx, "oversized_tail")
}

func tailTokensForKeep(live []llm.Message, keep int, charsPerToken float64) (int, bool) {
	cut := buildOwnCut(live, keep)
	if !cut.ok || cut.compactAll {
		return 0, false
	}
	chars := 0
	for _, m := range live[cut.firstKept:] {
		chars += EstimateMessageChars(m)
	}
	return EstimateTokensFromChars(chars, charsPerToken), true
}

// resolveSmartKeep boosts the default keep when the tail is small, stopping at
// the compact-all boundary. Explicit keep:N is always respected.
func resolveSmartKeep(live []llm.Message, requested *int, explicit, smart bool, minTokens, maxTokens int, charsPerToken float64) (int, bool, int) {
	baseKeep := 1
	if requested != nil {
		baseKeep = *requested
	}
	if explicit || !smart {
		return baseKeep, false, baseKeep
	}
	baseTokens, ok := tailTokensForKeep(live, baseKeep, charsPerToken)
	if !ok || baseTokens > minTokens {
		return baseKeep, false, baseKeep
	}
	baseCut := buildOwnCut(live, baseKeep)
	total := 0
	if baseCut.ok {
		total = baseCut.totalTurns
	}
	selected := baseKeep
	for k := baseKeep + 1; k <= total; k++ {
		tokens, ok := tailTokensForKeep(live, k, charsPerToken)
		if !ok || tokens > maxTokens {
			break
		}
		selected = k
	}
	return selected, selected != baseKeep, baseKeep
}

// ResolveCut resolves the compaction boundary for a live window.
func ResolveCut(live []llm.Message, opts Options) (Cut, bool) {
	opts = opts.withDefaults()

	var requested *int
	if opts.Explicit {
		k := opts.Keep
		requested = &k
	}
	keep, smartAdjusted, fromKeep := resolveSmartKeep(live, requested, opts.Explicit, opts.SmartKeep,
		opts.MinSmartTailTokens, opts.MaxSmartTailTokens, opts.CharsPerToken)

	cut := buildOwnCut(live, keep)
	if !cut.ok {
		return Cut{}, false
	}
	if !opts.Explicit {
		cut = applyTailBudget(live, cut, opts.MaxSmartTailTokens, opts.OversizedTailFactor, opts.CharsPerToken)
	}
	return Cut{
		FirstKept:     cut.firstKept,
		CompactAll:    cut.compactAll,
		KeptTurns:     cut.keptTurns,
		TotalTurns:    cut.totalTurns,
		BudgetCut:     cut.budgetCut,
		Fallback:      cut.fallback,
		SmartAdjusted: smartAdjusted,
		FromKeep:      fromKeep,
	}, true
}
