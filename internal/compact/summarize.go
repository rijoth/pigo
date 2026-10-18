package compact

import (
	"regexp"
	"strings"

	"pigo/internal/llm"
)

var headerNames = []string{"Session Goal", "Files And Changes", "Commits", "Tracked Commands", "Outstanding Context", "User Preferences"}

const summarySeparator = "\n\n---\n\n"

// sectionOf extracts one bracketed section from summary text.
func sectionOf(text, header string) string {
	tag := "[" + header + "]"
	start := strings.Index(text, tag)
	if start < 0 {
		return ""
	}
	after := text[start:]
	nextSection := -1
	for _, h := range headerNames {
		if h == header {
			continue
		}
		if n := strings.Index(after, "["+h+"]"); n > 0 {
			if nextSection < 0 || n < nextSection {
				nextSection = n
			}
		}
	}
	nextSep := strings.Index(after, "\n\n---\n\n")
	candidates := []int{}
	if nextSection > 0 {
		candidates = append(candidates, nextSection)
	}
	if nextSep > 0 {
		candidates = append(candidates, nextSep)
	}
	end := -1
	for _, c := range candidates {
		if end < 0 || c < end {
			end = c
		}
	}
	if end >= 0 {
		return strings.TrimSpace(after[:end])
	}
	return strings.TrimSpace(after)
}

// briefOf extracts the transcript after the final section separator.
func briefOf(text string) string {
	idx := strings.Index(text, summarySeparator)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(text[idx+len(summarySeparator):])
}

func isCleanLine(l string) bool {
	return strings.HasPrefix(l, "- ") && !strings.Contains(l, "<skill") && !strings.Contains(l, "</skill")
}

// mergeHeaderSection merges one section, keeping Outstanding Context fresh-only.
func mergeHeaderSection(header, prev, fresh string) string {
	if header == "Outstanding Context" {
		return fresh
	}
	if prev == "" {
		return fresh
	}
	if fresh == "" {
		return prev
	}
	if header == "Files And Changes" {
		return mergeFileLines(prev, fresh)
	}
	if header == "Tracked Commands" {
		return mergeTrackedCommandLines(prev, fresh)
	}
	var prevLines, freshLines []string
	for _, l := range strings.Split(prev, "\n") {
		if isCleanLine(l) {
			prevLines = append(prevLines, l)
		}
	}
	for _, l := range strings.Split(fresh, "\n") {
		if isCleanLine(l) {
			freshLines = append(freshLines, l)
		}
	}
	combined := []string{}
	seen := map[string]bool{}
	for _, l := range append(prevLines, freshLines...) {
		if seen[l] {
			continue
		}
		seen[l] = true
		combined = append(combined, l)
	}
	cap := 15
	if header == "Session Goal" || header == "Commits" {
		cap = 8
	}
	if len(combined) > cap {
		combined = combined[len(combined)-cap:]
	}
	if len(combined) == 0 {
		return ""
	}
	return "[" + header + "]\n" + strings.Join(combined, "\n")
}

// mergeCategorizedLines unions category items across compactions.
func mergeCategorizedLines(categories []string, prev, fresh, splitOn string, touchOnDup bool) map[string][]string {
	merged := map[string][]string{}
	present := map[string]map[string]bool{}
	for _, cat := range categories {
		merged[cat] = nil
		present[cat] = map[string]bool{}
	}
	for _, text := range []string{prev, fresh} {
		for _, line := range strings.Split(text, "\n") {
			for _, cat := range categories {
				prefix := "- " + cat + ": "
				if !strings.HasPrefix(line, prefix) {
					continue
				}
				rest := stripCapMarker(line[len(prefix):])
				for _, p := range strings.Split(rest, splitOn) {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					if touchOnDup && present[cat][p] {
						var next []string
						for _, e := range merged[cat] {
							if e != p {
								next = append(next, e)
							}
						}
						merged[cat] = next
					}
					if !present[cat][p] {
						present[cat][p] = true
						merged[cat] = append(merged[cat], p)
					}
				}
			}
		}
	}
	return merged
}

func formatCategorizedLines(header string, merged map[string][]string, categories []string, joinWith string, itemLimit int, keep string) string {
	var lines []string
	for _, cat := range categories {
		if len(merged[cat]) > 0 {
			lines = append(lines, "- "+cat+": "+capItems(merged[cat], itemLimit, joinWith, keep))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "[" + header + "]\n" + strings.Join(lines, "\n")
}

var fileCategories = []string{"Modified", "Created", "Read"}

func mergeFileLines(prev, fresh string) string {
	merged := mergeCategorizedLines(fileCategories, prev, fresh, ",", false)
	// If a path is Modified, it cannot also be Created.
	modified := map[string]bool{}
	for _, p := range merged["Modified"] {
		modified[p] = true
	}
	var created []string
	for _, p := range merged["Created"] {
		if !modified[p] {
			created = append(created, p)
		}
	}
	merged["Created"] = created
	return formatCategorizedLines("Files And Changes", merged, fileCategories, ", ", 10, "head")
}

var categoryNameRe = regexp.MustCompile(`^- ([^:]+): `)

func discoverCategoryNames(text string) []string {
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if m := categoryNameRe.FindStringSubmatch(line); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	return names
}

func mergeTrackedCommandLines(prev, fresh string) string {
	var categories []string
	seen := map[string]bool{}
	for _, name := range append(discoverCategoryNames(prev), discoverCategoryNames(fresh)...) {
		if !seen[name] {
			seen[name] = true
			categories = append(categories, name)
		}
	}
	merged := mergeCategorizedLines(categories, prev, fresh, commandSeparator, true)
	return formatCategorizedLines("Tracked Commands", merged, categories, commandSeparator, commandsPerName, "tail")
}

func mergeBriefTranscript(prev, fresh string) string {
	if prev == "" {
		return fresh
	}
	if fresh == "" {
		return prev
	}
	return prev + "\n\n" + fresh
}

func briefLineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}

func capBriefToLineBudget(text string, maxLines int) string {
	if text == "" || maxLines <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	kept := lines[len(lines)-maxLines:]
	firstHeader := -1
	for i, l := range kept {
		if sectionHeadRe.MatchString(l) {
			firstHeader = i
			break
		}
	}
	clean := kept
	if firstHeader > 0 {
		clean = kept[firstHeader:]
	}
	omitted := len(lines) - len(clean)
	return "...(" + itoa(omitted) + " earlier lines omitted)\n\n" + strings.Join(clean, "\n")
}

func mergeBriefTranscriptWithFreshBudget(prev, fresh string) string {
	if prev == "" {
		return fresh
	}
	if fresh == "" {
		return capBrief(prev)
	}
	freshLines := briefLineCount(fresh)
	remaining := briefMaxLines - freshLines
	if remaining < 0 {
		remaining = 0
	}
	prevTail := capBriefToLineBudget(prev, remaining)
	if prevTail == "" {
		return fresh
	}
	return prevTail + "\n\n" + fresh
}

func mergePrevious(prev, fresh string, preserveFreshBrief bool) string {
	continuationRe := regexp.MustCompile(`\n[ \t]+(\S)`)
	var headers []string
	for _, header := range headerNames {
		freshSec := sectionOf(fresh, header)
		prevSec := continuationRe.ReplaceAllString(sectionOf(prev, header), " $1")
		if s := mergeHeaderSection(header, prevSec, freshSec); s != "" {
			headers = append(headers, s)
		}
	}
	prevBrief := briefOf(prev)
	freshBrief := briefOf(fresh)
	var mergedBrief string
	if preserveFreshBrief {
		mergedBrief = mergeBriefTranscriptWithFreshBudget(prevBrief, freshBrief)
	} else {
		mergedBrief = mergeBriefTranscript(prevBrief, freshBrief)
	}
	var parts []string
	if len(headers) > 0 {
		parts = append(parts, strings.Join(headers, "\n\n"))
	}
	if mergedBrief != "" {
		if preserveFreshBrief {
			parts = append(parts, mergedBrief)
		} else {
			parts = append(parts, capBrief(mergedBrief))
		}
	}
	return strings.Join(parts, summarySeparator)
}

type compileOptions struct {
	briefBlocksFor            func([]Block) []Block
	capFreshBrief             bool
	preserveFreshBriefOnMerge bool
}

func compileWithBriefBlocks(in Input, opts compileOptions) string {
	blocks := FilterNoise(Normalize(in.History, in.SourceIndices))
	var briefBlocks []Block
	if opts.briefBlocksFor != nil {
		briefBlocks = opts.briefBlocksFor(blocks)
	}
	data := BuildSections(blocks, briefBlocks, in.FileOps, in.TrackCommands)
	fresh := formatSummary(data, opts.capFreshBrief)
	prev := ""
	if in.Previous != "" {
		prev = stripRecallNote(in.Previous)
	}
	merged := fresh
	if prev != "" {
		merged = mergePrevious(prev, fresh, opts.preserveFreshBriefOnMerge)
	}
	if merged == "" {
		return ""
	}
	return WrapLongLines(merged+summarySeparator+recallNote, 0)
}

// Input is one compilation request.
type Input struct {
	History       []llm.Message
	Cut           int // summarize History[:Cut]
	Previous      string
	SourceIndices []int
	FileOps       *FileOps
	TrackCommands []string
}

// Compile renders an unranked summary of Input.History[:Input.Cut].
func Compile(in Input) string {
	if in.Cut < len(in.History) {
		in.History = in.History[:in.Cut]
	}
	return compileWithBriefBlocks(in, compileOptions{capFreshBrief: true})
}

// CompileRanked renders a summary using ranked brief-block selection.
func CompileRanked(in Input, opts Options) string {
	opts = opts.withDefaults()
	if in.Cut < len(in.History) {
		in.History = in.History[:in.Cut]
	}
	return compileWithBriefBlocks(in, compileOptions{
		briefBlocksFor: func(blocks []Block) []Block {
			return SelectRankedBriefBlocks(blocks, BriefRankingOptions{
				FileOps:              in.FileOps,
				MaxBriefChars:        int(float64(opts.RankedBudgetTokens) * opts.CharsPerToken),
				MaxBriefCharsCeiling: int(float64(opts.RankedCeilingTokens) * opts.CharsPerToken),
				BriefCharsPerBlock:   int(float64(opts.RankedTokensPerBlock) * opts.CharsPerToken),
			})
		},
		capFreshBrief:             false,
		preserveFreshBriefOnMerge: true,
	})
}

var recallNoteRe = regexp.MustCompile(`\n*(?:---\n+)?` + recallNotePattern() + `\n*`)

func recallNotePattern() string {
	words := regexp.MustCompile(`\s+`).Split(recallNote, -1)
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	return strings.Join(words, `\s+`)
}

// stripRecallNote removes any stored RECALL_NOTE before a summary is merged.
func stripRecallNote(text string) string {
	return strings.TrimRight(recallNoteRe.ReplaceAllString(text, "\n\n"), " \t\n")
}
