package models

// ThinkingLevels is the ordered set of reasoning levels, matching Pi.
var ThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// SupportedThinkingLevels returns the levels the entry accepts, in order. A
// model without reasoning supports only "off". xhigh and max require an
// explicit thinkingLevelMap value. A null mapping for a standard level means
// "no explicit provider value", not "unsupported": the level stays selectable
// and uses the provider default.
func (e ModelEntry) SupportedThinkingLevels() []string {
	if !e.Reasoning {
		return []string{"off"}
	}
	levels := make([]string, 0, len(ThinkingLevels))
	for _, level := range ThinkingLevels {
		if level == "xhigh" || level == "max" {
			if v, ok := e.ThinkingLevelMap[level]; !ok || v == nil {
				continue
			}
		}
		levels = append(levels, level)
	}
	return levels
}

// ClampThinkingLevel returns level when supported, otherwise the nearest
// supported level: later levels first, then earlier ones.
func (e ModelEntry) ClampThinkingLevel(level string) string {
	levels := e.SupportedThinkingLevels()
	for _, l := range levels {
		if l == level {
			return l
		}
	}
	requested := levelIndex(level)
	if requested < 0 {
		return levels[0]
	}
	for i := requested; i < len(ThinkingLevels); i++ {
		for _, l := range levels {
			if l == ThinkingLevels[i] {
				return l
			}
		}
	}
	for i := requested - 1; i >= 0; i-- {
		for _, l := range levels {
			if l == ThinkingLevels[i] {
				return l
			}
		}
	}
	return levels[0]
}

// NextThinkingLevel returns the level after current, wrapping around. It
// returns the first level when current is not in the list.
func NextThinkingLevel(levels []string, current string) string {
	if len(levels) == 0 {
		return "off"
	}
	for i, l := range levels {
		if l == current {
			return levels[(i+1)%len(levels)]
		}
	}
	return levels[0]
}

func levelIndex(level string) int {
	for i, l := range ThinkingLevels {
		if l == level {
			return i
		}
	}
	return -1
}
