package markdown

import (
	"strings"

	"charm.land/glamour/v2"
	glamourstyles "charm.land/glamour/v2/styles"
	"github.com/alecthomas/chroma/v2/quick"

	"pigo/internal/config"
)

// markdownSplitter groups streamed markdown into whole blocks and streams code
// fences line by line. A markdown block ends at a blank line outside a fence.
// It does no rendering itself: tests drive it with collected callbacks.
type markdownSplitter struct {
	partial strings.Builder
	block   strings.Builder
	inFence bool

	onBlock     func(string)
	onCodeStart func(string)
	onCodeLine  func(string)
	onCodeEnd   func()
}

func (m *markdownSplitter) Write(s string) {
	m.partial.WriteString(s)
	for {
		line, rest, ok := strings.Cut(m.partial.String(), "\n")
		if !ok {
			return
		}
		m.partial.Reset()
		m.partial.WriteString(rest)
		m.line(line)
	}
}

func (m *markdownSplitter) line(line string) {
	trimmed := strings.TrimSpace(line)
	if m.inFence {
		if isFence(trimmed) {
			m.endCode()
			return
		}
		if m.onCodeLine != nil {
			m.onCodeLine(line)
		}
		return
	}
	if isFence(trimmed) {
		m.flushBlock()
		m.inFence = true
		if m.onCodeStart != nil {
			m.onCodeStart(fenceLang(trimmed))
		}
		return
	}
	if trimmed == "" {
		m.flushBlock()
		return
	}
	m.block.WriteString(line)
	m.block.WriteByte('\n')
}

// Flush emits any buffered partial line, closes an open fence, and emits the
// final block. It is safe to call more than once.
func (m *markdownSplitter) Flush() {
	if s := m.partial.String(); s != "" {
		m.partial.Reset()
		m.line(s)
	}
	m.endCode()
	m.flushBlock()
}

func (m *markdownSplitter) endCode() {
	if !m.inFence {
		return
	}
	m.inFence = false
	if m.onCodeEnd != nil {
		m.onCodeEnd()
	}
}

func (m *markdownSplitter) flushBlock() {
	s := m.block.String()
	m.block.Reset()
	if strings.TrimSpace(s) == "" {
		return
	}
	if m.onBlock != nil {
		m.onBlock(s)
	}
}

// isFence reports whether a trimmed line opens or closes a code fence.
func isFence(s string) bool {
	return strings.HasPrefix(s, "```") || strings.HasPrefix(s, "~~~")
}

// fenceLang extracts the info string after an opening fence.
func fenceLang(fence string) string {
	return strings.TrimSpace(strings.TrimLeft(fence, "`~"))
}

// Renderer renders completed blocks with glamour and streams fenced
// code with chroma. The splitter decides what is a block; this type decides
// how each piece looks.
type Renderer struct {
	color    bool
	style    string // chroma style name
	dim      func(string) string
	emit     func(string)
	split    *markdownSplitter
	renderer *glamour.TermRenderer
	lang     string
}

func NewRenderer(width int, color, narrow bool, dim func(string) string, emit func(string)) *Renderer {
	m := &Renderer{color: color, style: chromaStyle(config.Theme), dim: dim, emit: emit}
	if r := newGlamour(width, color, narrow); r != nil {
		m.renderer = r
	}
	m.split = &markdownSplitter{
		onBlock:     m.block,
		onCodeStart: m.codeStart,
		onCodeLine:  m.codeLine,
	}
	return m
}

// newGlamour builds the markdown renderer from the config theme. Narrow
// terminals drop the document's left margin, and color-less output uses the
// notty stylesheet. The terminal is never queried.
func newGlamour(width int, color, narrow bool) *glamour.TermRenderer {
	name := glamourStyle(config.Theme, color)
	opts := []glamour.TermRendererOption{glamour.WithWordWrap(width)}
	if cfg, ok := glamourstyles.DefaultStyles[name]; ok {
		c := *cfg
		if narrow {
			zero := uint(0)
			c.Document.Margin = &zero
		}
		opts = append(opts, glamour.WithStyles(c))
	} else {
		opts = append(opts, glamour.WithStandardStyle(name))
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return nil
	}
	return r
}

func (m *Renderer) Write(s string) { m.split.Write(s) }

func (m *Renderer) Flush() { m.split.Flush() }

func (m *Renderer) block(s string) {
	if m.renderer == nil {
		m.emit(s)
		return
	}
	out, err := m.renderer.Render(s)
	if err != nil {
		m.emit(s)
		return
	}
	m.emit(out)
}

func (m *Renderer) codeStart(lang string) {
	if lang == "" {
		lang = "text"
	}
	m.lang = lang
	m.emit(m.dim("```"+lang) + "\n")
}

func (m *Renderer) codeLine(line string) {
	if !m.color || line == "" {
		m.emit(line + "\n")
		return
	}
	m.emit(highlight(m.style, m.lang, line) + "\n")
}

// glamourStyle picks the glamour stylesheet. It never queries the terminal;
// the theme is a config constant.
func glamourStyle(theme string, color bool) string {
	if !color {
		return "notty"
	}
	if theme == "light" {
		return "light"
	}
	return "dark"
}

// chromaStyle maps the config theme to a chroma style name.
func chromaStyle(theme string) string {
	if theme == "light" {
		return "github"
	}
	return "monokai"
}

// highlight renders one line with the terminal256 chroma formatter. Chroma is
// already a glamour dependency; importing it directly is what lets us
// highlight a fence line as it arrives instead of after the closing fence.
func highlight(style, lang, line string) string {
	var b strings.Builder
	if err := quick.Highlight(&b, line, lang, "terminal256", style); err != nil {
		return line
	}
	return strings.TrimRight(b.String(), "\n")
}
