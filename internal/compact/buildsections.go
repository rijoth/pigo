package compact

import "regexp"

var blockerRe = regexp.MustCompile(`(?i)\b(fail(ed|s|ure|ing)?|broken|cannot|can't|won't work|does not work|doesn't work|still (broken|failing|wrong)|blocked|blocker|not (fixed|resolved|working)|crash(es|ed|ing)?)\b`)

func extractOutstandingContext(blocks []Block) []string {
	var items []string
	tail := blocks
	if len(tail) > 20 {
		tail = tail[len(tail)-20:]
	}
	for _, b := range tail {
		if b.Kind != KindAssistant && b.Kind != KindUser {
			continue
		}
		for _, line := range nonEmptyLines(b.Text) {
			if !blockerRe.MatchString(line) {
				continue
			}
			if len([]rune(line)) < 15 {
				continue
			}
			if regexp.MustCompile(`^\s*[-*+>]\s`).MatchString(line) || regexp.MustCompile(`^\s*\(`).MatchString(line) {
				continue
			}
			if !regexp.MustCompile("^\\s*[\"'`*_]?[A-Z`]").MatchString(line) {
				continue
			}
			clipped := clipSentence(line, 150)
			if b.Kind == KindUser {
				clipped = "[user] " + clipped
			}
			found := false
			for _, it := range items {
				if it == clipped {
					found = true
					break
				}
			}
			if !found {
				items = append(items, clipped)
			}
			break
		}
	}
	if len(items) > 5 {
		items = items[:5]
	}
	return items
}

func formatFileActivity(blocks []Block, fileOps *FileOps) []string {
	act := ExtractFiles(blocks, fileOps)
	for p := range act.modified {
		delete(act.created, p)
	}
	capSet := func(set map[string]bool, limit int) string {
		keys := sortedKeys(set)
		if len(keys) <= limit {
			return joinStrings(keys, ", ")
		}
		return joinStrings(keys[:limit], ", ") + " (+" + itoa(len(keys)-limit) + " more)"
	}
	var lines []string
	if len(act.modified) > 0 {
		lines = append(lines, "Modified: "+capSet(act.modified, 10))
	}
	if len(act.created) > 0 {
		lines = append(lines, "Created: "+capSet(act.created, 10))
	}
	if len(act.read) > 0 {
		lines = append(lines, "Read: "+capSet(act.read, 10))
	}
	return lines
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// BuildSections extracts every structured section from normalized blocks.
func BuildSections(blocks, briefBlocks []Block, fileOps *FileOps, trackCommands []string) sectionData {
	if briefBlocks == nil {
		briefBlocks = blocks
	}
	briefSections := BuildBriefSections(briefBlocks)
	sessionGoal := ExtractGoals(blocks)
	userPreferences := DedupPreferencesAgainstGoals(ExtractPreferences(blocks), sessionGoal)
	return sectionData{
		sessionGoal:        sessionGoal,
		outstandingContext: extractOutstandingContext(blocks),
		filesAndChanges:    formatFileActivity(blocks, fileOps),
		commits:            FormatCommits(ExtractCommits(blocks), 8),
		userPreferences:    userPreferences,
		trackedCommands:    FormatTrackedCommands(ExtractTrackedCommands(blocks, trackCommands)),
		briefTranscript:    StringifyBrief(briefSections),
	}
}

func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
