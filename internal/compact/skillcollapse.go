package compact

import "regexp"

var (
	skillTagRe   = regexp.MustCompile(`^-?\s*<skill\s+name="([^"]+)"`)
	skillCloseRe = regexp.MustCompile(`^-?\s*</skill>`)
	skillBlockRe = regexp.MustCompile(`(?s)<skill\s+name="([^"]+)"[^>]*>.*?(?:</skill>|$)`)
)

// collapseSkillLines replaces each <skill> block with a single marker line and
// drops the block body, deduping by name.
func collapseSkillLines(lines []string) []string {
	var result []string
	seen := map[string]bool{}
	inside := false
	for _, line := range lines {
		if m := skillTagRe.FindStringSubmatch(line); m != nil {
			inside = true
			if !seen[m[1]] {
				seen[m[1]] = true
				result = append(result, "[skill: "+m[1]+"]")
			}
			continue
		}
		if inside {
			if skillCloseRe.MatchString(line) {
				inside = false
			}
			continue
		}
		result = append(result, line)
	}
	return result
}

// collapseSkillText collapses <skill> blocks in raw text.
func collapseSkillText(text string) string {
	return skillBlockRe.ReplaceAllString(text, "[skill: $1]")
}
