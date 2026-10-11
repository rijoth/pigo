// Package diff renders small unified diffs for tool output and previews the
// change an edit or write would make before it runs.
package diff

import (
	"fmt"
	"os"
	"strings"
)

// diffOp is one line of an edit script.
type diffOp struct {
	kind byte // ' ' equal, '-' delete, '+' insert
	text string
	old  int // 1-based line in the old file, 0 if none
	new  int // 1-based line in the new file, 0 if none
}

// lcsOps computes an edit script between old and new with a plain longest
// common subsequence. Hunks are small, so the quadratic table is fine.
func lcsOps(old, new []string) []diffOp {
	n, m := len(old), len(new)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case old[i] == new[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case old[i] == new[j]:
			ops = append(ops, diffOp{' ', old[i], i + 1, j + 1})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{'-', old[i], i + 1, 0})
			i++
		default:
			ops = append(ops, diffOp{'+', new[j], 0, j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', old[i], i + 1, 0})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', new[j], 0, j + 1})
	}
	return ops
}

// Format returns a unified-style diff of old against new, keeping at most
// context unchanged lines around each change. The output is plain text, one
// line per element, with line numbers and +/-/space prefixes.
func Format(old, new []string, context int) []string {
	// Trim the common prefix and suffix so the LCS only sees the changed
	// region. This keeps the quadratic table small for a localized edit in a
	// large file, while the trimmed lines still provide real context.
	prefix := 0
	for prefix < len(old) && prefix < len(new) && old[prefix] == new[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(new)-prefix &&
		old[len(old)-1-suffix] == new[len(new)-1-suffix] {
		suffix++
	}
	ops := make([]diffOp, 0, len(old)+len(new))
	for i := 0; i < prefix; i++ {
		ops = append(ops, diffOp{' ', old[i], i + 1, i + 1})
	}
	for _, op := range lcsOps(old[prefix:len(old)-suffix], new[prefix:len(new)-suffix]) {
		op.old += prefix
		op.new += prefix
		ops = append(ops, op)
	}
	for i := 0; i < suffix; i++ {
		oldNo := len(old) - suffix + i + 1
		newNo := len(new) - suffix + i + 1
		ops = append(ops, diffOp{' ', old[oldNo-1], oldNo, newNo})
	}
	var hunks [][2]int
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		j := i
		for j < len(ops) && ops[j].kind != ' ' {
			j++
		}
		hunks = append(hunks, [2]int{i, j})
		i = j
	}
	if len(hunks) == 0 {
		return nil
	}
	// Merge hunks whose context would touch or overlap.
	merged := [][2]int{hunks[0]}
	for _, h := range hunks[1:] {
		last := &merged[len(merged)-1]
		if h[0]-context <= last[1]+context {
			last[1] = h[1]
		} else {
			merged = append(merged, h)
		}
	}
	var out []string
	for _, h := range merged {
		start := max(0, h[0]-context)
		end := min(len(ops), h[1]+context)
		oldStart, newStart, oldCount, newCount := 0, 0, 0, 0
		for _, op := range ops[start:end] {
			if op.old > 0 && oldStart == 0 {
				oldStart = op.old
			}
			if op.new > 0 && newStart == 0 {
				newStart = op.new
			}
			if op.kind != '+' {
				oldCount++
			}
			if op.kind != '-' {
				newCount++
			}
		}
		if oldStart == 0 {
			oldStart = 1
		}
		if newStart == 0 {
			newStart = 1
		}
		out = append(out, fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount))
		for _, op := range ops[start:end] {
			num := op.new
			if op.kind == '-' {
				num = op.old
			}
			out = append(out, fmt.Sprintf("%c%5d %s", op.kind, num, op.text))
		}
	}
	return out
}

// SplitLines splits text into lines, dropping one trailing newline.
func SplitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// EditPreview returns the pre-edit and post-edit line slices for replacing old
// with new in the file at path, without modifying it. ok is false when the
// file cannot be read or old does not occur exactly once, matching the edit
// tool's contract.
func EditPreview(path, old, new string) (pre, post []string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil || old == "" || strings.Count(string(data), old) != 1 {
		return nil, nil, false
	}
	content := string(data)
	return SplitLines(content), SplitLines(strings.Replace(content, old, new, 1)), true
}

// WritePreview returns the current file contents and the content that would be
// written, as line slices. A missing file yields an empty pre slice. ok is
// false when the path is empty or the file cannot be read for another reason.
func WritePreview(path, content string) (pre, post []string, ok bool) {
	if path == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, false
	}
	return SplitLines(string(data)), SplitLines(content), true
}

// Hunk reconstructs the pre-edit and post-edit line slices from the file on
// disk. It finds the applied new text and puts the old text back, which gives
// real surrounding context and line numbers. ok is false when the file cannot
// be read or the edit cannot be reversed.
func Hunk(path, old, new string) (pre, post []string, ok bool) {
	if new == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	before := strings.Replace(string(data), new, old, 1)
	if before == string(data) {
		return nil, nil, false
	}
	return SplitLines(before), SplitLines(string(data)), true
}
