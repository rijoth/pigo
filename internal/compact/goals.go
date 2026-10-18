package compact

import (
	"regexp"
	"strings"
)

var (
	scopeChangeRe    = regexp.MustCompile(`(?i)\b(instead|actually|change of plan|forget that|new task|switch to|now I want|pivot|let'?s do|stop .* and)\b`)
	taskRe           = regexp.MustCompile(`(?i)\b(fix|implement|add|create|build|refactor|debug|investigate|update|remove|delete|migrate|deploy|test|write|set up)\b`)
	noiseShortRe     = regexp.MustCompile(`(?i)^(ok|yes|no|sure|yeah|yep|go|hi|hey|thx|thanks|ok\b.*|y|n|k)\s*[.!?]*$`)
	nonGoalRe        = regexp.MustCompile("^\\s*[\\[\u2502\u251c\u2514\u2500\u256d\u2570]|```|^\\s*(=[A-Z]+\\(|function |const |let |var |import |export |class )|^(https?:|file:|/[A-Za-z])|\\\\n|^\\s*For each\\b|\\bin full\\b[^\\n]*\\b(comments|issue|issues|PRs?|linked)\\b")
	templateSignalRe = regexp.MustCompile(`(?i)^\s*(For each\b|Do NOT implement\b|Analyze and propose\b|If Task/context\b|Output:\s*$)`)
	leadingBulletRe  = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+`)
)

const (
	maxGoalChars = 200
	leadingChars = 200
)

func truncateAtTemplate(lines []string) []string {
	for i, l := range lines {
		if templateSignalRe.MatchString(l) {
			return lines[:i]
		}
	}
	return lines
}

func stripLeadingBullet(line string) string {
	return strings.TrimSpace(leadingBulletRe.ReplaceAllString(line, ""))
}

func isSubstantiveGoal(text string) bool {
	t := strings.TrimSpace(text)
	if len([]rune(t)) <= 5 || len([]rune(t)) > maxGoalChars {
		return false
	}
	if noiseShortRe.MatchString(t) || nonGoalRe.MatchString(t) {
		return false
	}
	return true
}

// ExtractGoals finds user goals, preferring the latest scope change.
func ExtractGoals(blocks []Block) []string {
	var goals []string
	var latestScopeChange []string

	for _, b := range blocks {
		if b.Kind != KindUser {
			continue
		}
		rawLines := nonEmptyLines(b.Text)
		truncated := truncateAtTemplate(rawLines)
		var filtered []string
		for _, l := range truncated {
			if isSubstantiveGoal(l) {
				filtered = append(filtered, l)
			}
		}
		collapsed := collapseSkillLines(filtered)
		var lines []string
		for _, l := range collapsed {
			l = stripLeadingBullet(l)
			if len([]rune(l)) > 5 {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			continue
		}
		if len(goals) == 0 {
			goals = append(goals, lines[:min(6, len(lines))]...)
			continue
		}
		leading := sliceRunes(b.Text, leadingChars)
		if scopeChangeRe.MatchString(leading) {
			latestScopeChange = clipAll(lines[:min(3, len(lines))], maxGoalChars)
		} else if taskRe.MatchString(leading) && len([]rune(lines[0])) > 15 {
			latestScopeChange = clipAll(lines[:min(2, len(lines))], maxGoalChars)
		}
	}

	if len(latestScopeChange) > 0 {
		goals = append(goals, "[Scope change]")
		goals = append(goals, latestScopeChange...)
	}
	if len(goals) > 8 {
		goals = goals[:8]
	}
	return goals
}

func clipAll(items []string, max int) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		out = append(out, clip(s, max))
	}
	return out
}

func sliceRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
