package compact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTruncateTokens(t *testing.T) {
	long := strings.Repeat("alpha beta gamma delta ", 100)
	got := truncateTokens(long, 10)
	if !strings.Contains(got, "...(truncated)") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	short := "alpha beta"
	if got := truncateTokens(short, 10); got != short {
		t.Fatalf("short text changed: %q", got)
	}
}

func TestCompressBashDropsHeredocBody(t *testing.T) {
	raw := "cat > f <<EOF\ncontent line\nEOF"
	if got := compressBash(raw); got != "cat > f <<EOF" {
		t.Fatalf("compressBash = %q", got)
	}
	preview := "python3 - <<PY\nprint(1)\nPY"
	if got := compressBash(preview); !strings.Contains(got, "print(1)") {
		t.Fatalf("expected heredoc preview: %q", got)
	}
}

func TestBriefCapsToolCallsKeepTail(t *testing.T) {
	var blocks []Block
	for i := 0; i < 10; i++ {
		args, _ := json.Marshal(map[string]string{"path": "file" + string(rune('a'+i)) + ".go"})
		blocks = append(blocks, Block{Kind: KindToolCall, Name: "edit", Args: decodeArgs(args), Src: i + 1})
	}
	out := StringifyBrief(BuildBriefSections(blocks))
	if !strings.Contains(out, "(2 earlier tool-call entries omitted)") {
		t.Fatalf("missing omission marker: %q", out)
	}
	if !strings.Contains(out, "filei.go") || !strings.Contains(out, "filej.go") {
		t.Fatalf("tail not kept: %q", out)
	}
	if strings.Contains(out, "filea.go") {
		t.Fatalf("head should be dropped: %q", out)
	}
}

func TestBriefCollapsesDuplicateToolLines(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "a.go"})
	blocks := []Block{
		{Kind: KindToolCall, Name: "edit", Args: decodeArgs(args), Src: 1},
		{Kind: KindToolCall, Name: "edit", Args: decodeArgs(args), Src: 2},
	}
	out := StringifyBrief(BuildBriefSections(blocks))
	if !strings.Contains(out, "x2") {
		t.Fatalf("expected collapsed tool line: %q", out)
	}
}
