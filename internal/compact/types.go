// Package compact implements deterministic, LLM-free session compaction.
// It ports pi-vcc's extraction and formatting pipeline: normalize messages
// into blocks, extract structured sections, and render a stable summary with
// a brief transcript. Nothing here calls a model.
package compact

// Kind classifies a normalized conversation block.
type Kind int

const (
	KindUser Kind = iota
	KindAssistant
	KindToolCall
	KindToolResult
)

// Block is one normalized piece of a conversation.
type Block struct {
	Kind Kind
	Text string         // user/assistant/tool_result text
	Name string         // tool name for tool_call/tool_result
	Args map[string]any // tool_call arguments
	Src  int            // session-global index; -1 means no ref
}

// FileOps is hook-provided file activity. pigo derives files from tool calls,
// so this stays nil in practice; it is kept for parity with the pi-vcc port.
type FileOps struct {
	Read     []string
	Modified []string
	Created  []string
}

// Options tunes compaction. Zero values fall back to the package defaults.
type Options struct {
	Keep          int  // requested user turns to keep
	Explicit      bool // user typed keep:N; respected absolutely
	SmartKeep     bool // boost the default keep when the tail is small
	TrackCommands []string

	MinSmartTailTokens  int
	MaxSmartTailTokens  int
	OversizedTailFactor float64
	CharsPerToken       float64

	RankedBudgetTokens   int
	RankedCeilingTokens  int
	RankedTokensPerBlock int
}

const (
	defaultMinSmartTailTokens   = 5000
	defaultMaxSmartTailTokens   = 25000
	defaultOversizedTailFactor  = 2.5
	defaultCharsPerToken        = 4.0
	defaultRankedBudgetTokens   = 1100
	defaultRankedCeilingTokens  = 2000
	defaultRankedTokensPerBlock = 15
)

// DefaultOptions returns the pi-vcc defaults with smart keep enabled.
func DefaultOptions() Options {
	return Options{
		Keep:                 1,
		SmartKeep:            true,
		MinSmartTailTokens:   defaultMinSmartTailTokens,
		MaxSmartTailTokens:   defaultMaxSmartTailTokens,
		OversizedTailFactor:  defaultOversizedTailFactor,
		CharsPerToken:        defaultCharsPerToken,
		RankedBudgetTokens:   defaultRankedBudgetTokens,
		RankedCeilingTokens:  defaultRankedCeilingTokens,
		RankedTokensPerBlock: defaultRankedTokensPerBlock,
	}
}

// withDefaults fills zero-valued option fields with the package defaults.
func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.Keep == 0 && !o.Explicit {
		o.Keep = d.Keep
	}
	if o.CharsPerToken <= 0 {
		o.CharsPerToken = d.CharsPerToken
	}
	if o.MinSmartTailTokens <= 0 {
		o.MinSmartTailTokens = d.MinSmartTailTokens
	}
	if o.MaxSmartTailTokens <= 0 {
		o.MaxSmartTailTokens = d.MaxSmartTailTokens
	}
	if o.OversizedTailFactor <= 0 {
		o.OversizedTailFactor = d.OversizedTailFactor
	}
	if o.RankedBudgetTokens <= 0 {
		o.RankedBudgetTokens = d.RankedBudgetTokens
	}
	if o.RankedCeilingTokens <= 0 {
		o.RankedCeilingTokens = d.RankedCeilingTokens
	}
	if o.RankedTokensPerBlock <= 0 {
		o.RankedTokensPerBlock = d.RankedTokensPerBlock
	}
	return o
}
