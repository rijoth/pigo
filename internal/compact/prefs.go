package compact

import (
	"regexp"
	"strings"
)

var prefPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bprefer(?:s|red|ring)?\s+\w`),
	regexp.MustCompile(`(?i)\bdon'?t want\b`),
	regexp.MustCompile(`(?i)\balways (?:use|do|run|prefer|keep|make|format|write|add|set|put|prefix|start|include|append)\b`),
	regexp.MustCompile(`(?i)\bnever (?:use|do|run|push|commit|write|ignore|add|set|put|remove|delete|include|deploy)\b`),
	regexp.MustCompile(`(?i)\bplease (?:use|avoid|keep|make|don'?t|do not|format|write)\b`),
	regexp.MustCompile(`(?i)\b(?:style|format|language|naming)\s*[:=]\s*\S`),
}

// ExtractPreferences finds explicit user preferences, one per user block.
func ExtractPreferences(blocks []Block) []string {
	var prefs []string
	seen := map[string]bool{}
	for _, b := range blocks {
		if b.Kind != KindUser {
			continue
		}
		perBlock := 0
		for _, line := range nonEmptyLines(b.Text) {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || len([]rune(trimmed)) < 5 || len([]rune(trimmed)) > 200 {
				continue
			}
			if strings.HasSuffix(trimmed, "?") || strings.Contains(trimmed, "?...") {
				continue
			}
			matched := false
			for _, p := range prefPatterns {
				if p.MatchString(trimmed) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			clipped := clip(trimmed, 200)
			key := strings.ToLower(clipped)
			if seen[key] {
				continue
			}
			seen[key] = true
			prefs = append(prefs, clipped)
			perBlock++
			if perBlock >= 1 {
				break
			}
		}
	}
	if len(prefs) > 10 {
		prefs = prefs[:10]
	}
	return prefs
}

// DedupPreferencesAgainstGoals removes preferences that duplicate a goal.
func DedupPreferencesAgainstGoals(prefs, goals []string) []string {
	goalSet := map[string]bool{}
	for _, g := range goals {
		goalSet[strings.ToLower(strings.TrimSpace(g))] = true
	}
	var out []string
	for _, p := range prefs {
		if goalSet[strings.ToLower(strings.TrimSpace(p))] {
			continue
		}
		out = append(out, p)
	}
	return out
}
