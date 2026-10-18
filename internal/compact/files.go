package compact

import "strings"

var (
	fileReadTools   = map[string]bool{"read": true, "read_file": true, "view": true}
	fileWriteTools  = map[string]bool{"edit": true, "write": true, "edit_file": true, "write_file": true, "multiedit": true, "quick_edit": true, "target_edit": true, "apply_patch": true}
	fileCreateTools = map[string]bool{"write": true, "write_file": true}
)

// fileActivity is the categorized set of paths touched by a conversation.
type fileActivity struct {
	read     map[string]bool
	modified map[string]bool
	created  map[string]bool
}

// longestCommonDirPrefix returns the longest common directory prefix of two or
// more absolute paths, requiring at least /a/b.
func longestCommonDirPrefix(paths []string) string {
	var split [][]string
	for _, p := range paths {
		if strings.HasPrefix(p, "/") {
			split = append(split, strings.Split(p, "/"))
		}
	}
	if len(split) < 2 {
		return ""
	}
	minLen := len(split[0])
	for _, s := range split {
		if len(s) < minLen {
			minLen = len(s)
		}
	}
	i := 0
	for i < minLen-1 {
		seg := split[0][i]
		same := true
		for _, s := range split {
			if s[i] != seg {
				same = false
				break
			}
		}
		if !same {
			break
		}
		i++
	}
	if i < 2 {
		return ""
	}
	return strings.Join(split[0][:i], "/") + "/"
}

func trimPathSet(set map[string]bool, prefix string) map[string]bool {
	if prefix == "" {
		return set
	}
	out := map[string]bool{}
	for p := range set {
		out[strings.TrimPrefix(p, prefix)] = true
	}
	return out
}

// ExtractFiles collects read/modified/created paths from tool calls and trims
// their common directory prefix.
func ExtractFiles(blocks []Block, fileOps *FileOps) fileActivity {
	act := fileActivity{read: map[string]bool{}, modified: map[string]bool{}, created: map[string]bool{}}
	if fileOps != nil {
		for _, p := range fileOps.Read {
			act.read[p] = true
		}
		for _, p := range fileOps.Modified {
			act.modified[p] = true
		}
		for _, p := range fileOps.Created {
			act.created[p] = true
		}
	}

	for _, b := range blocks {
		if b.Kind != KindToolCall {
			continue
		}
		p := extractPath(b.Args)
		if p == "" {
			continue
		}
		name := strings.ToLower(b.Name)
		if fileReadTools[name] {
			act.read[p] = true
		}
		if fileWriteTools[name] {
			act.modified[p] = true
		}
		if fileCreateTools[name] {
			act.created[p] = true
		}
	}

	var all []string
	for p := range act.read {
		all = append(all, p)
	}
	for p := range act.modified {
		all = append(all, p)
	}
	for p := range act.created {
		all = append(all, p)
	}
	if prefix := longestCommonDirPrefix(all); prefix != "" {
		act.read = trimPathSet(act.read, prefix)
		act.modified = trimPathSet(act.modified, prefix)
		act.created = trimPathSet(act.created, prefix)
	}
	return act
}
