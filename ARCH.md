# pigo: Architecture and Design

> **Audience:** LLM agents and humans implementing or modifying this project.
> **Status:** Design v1.
> **Read this whole document before writing code.** Section 11 lists hard rules for contributors.

---

## 1. What this is

`pigo` is a minimal terminal coding agent written in Go, in the spirit of Pi. It runs a loop: send the conversation to an LLM, execute any tool calls the model requests, feed results back, repeat until the model answers without tool calls.

It prints to normal terminal stdout (scrollback works, copy/paste works), reads input with a line editor, and styles output with lipgloss and glamour. It is **not** a fullscreen TUI.

## 2. Goals and non-goals

### Goals

- Small, readable, hackable. A competent developer should read the entire codebase in under an hour.
- Provider-agnostic: swapping Anthropic, OpenAI, OpenRouter, Gemini, or a local model is a config change.
- Four core tools: `read`, `write`, `edit`, `bash`. The model can do nearly everything with these.
- Works interactively and non-interactively (`pigo -p "prompt"` for scripts and pipes).
- Sessions are plain text files that can be resumed, inspected with `jq`, and diffed.

### Non-goals (do not add these)

- Fullscreen TUI, mouse support, panes, themes engine.
- Plugin systems, dynamic loading, scripting languages, config DSLs.
- Sub-agents, planning modes, todo trackers, built-in web search, MCP client. (If wanted later, they are added by editing source; see section 10.)
- Sandboxing. The tool trusts the model the way the user trusts a shell.
- Telemetry, auto-update, analytics.

## 3. Design principles (suckless-inspired)

1. **Simplicity over features.** Every feature must justify its lines of code. When in doubt, leave it out.
2. **Small and finite.** There is a line budget (section 12). Exceeding it is a design smell that requires discussion, not a quiet merge.
3. **Configuration is source code.** Defaults live in `internal/config/config.go` as constants, like `config.h`. Edit and rebuild to customize. Only secrets and per-run choices (API keys, model, flags) come from env vars and flags. No config file parser.
4. **Do one thing per package.** Packages communicate through small interfaces and plain structs. No package knows about the UI except `ui`.
5. **Plain text everywhere.** Sessions are JSONL. Logs are text. No databases, no binary formats.
6. **Unix citizenship.** stdin/stdout/stderr are used correctly: output to stdout, diagnostics to stderr, meaningful exit codes, composable in pipes.
7. **Minimal dependencies.** Standard library first. Allowed third-party dependencies are listed in section 9. Adding a dependency needs a written justification in the PR description.
8. **No abstraction before the second use.** Interfaces exist only at real seams (provider, tool). Everything else is concrete.
9. **Boring code.** No reflection tricks, no generics gymnastics, no code generation, no global mutable state beyond `main`.
10. **Errors are values; fail loudly.** Return errors, add context with `fmt.Errorf("...: %w", err)`, print them to the user. Do not swallow errors.

## 4. Architecture overview

```
                    +-----------------------------+
                    |        cmd/pigo/main.go       |
                    |  flags, wiring, main loop   |
                    +--------------+--------------+
                                   |
         +-------------------------+-------------------------+
         |                         |                         |
 +-------v-------+        +--------v--------+        +-------v-------+
 |      ui       |        |      agent      |        |    session    |
 | readline,     |<-event-|  the loop       |------->| JSONL store   |
 | lipgloss,     |        |                 |        | compaction    |
 | glamour       |        +----+-------+----+        +---------------+
 +---------------+             |       |
                               |       |
                  +------------v--+  +-v-------------+
                  |      llm      |  |     tools     |
                  | Provider      |  | read write    |
                  | interface +   |  | edit bash     |
                  | Fantasy       |  +---------------+
                  | adapter       |
                  +---------------+
```

**Dependency direction (strict, enforced by review):**

```
main -> ui, agent, session, config, auth, models, providers, fantasy
agent -> llm, tools, event
ui -> event, llm, config, markdown, selector, diff
markdown -> config
selector -> (stdlib only)
diff -> (stdlib only)
models -> (stdlib only)
providers -> models, auth
fantasy -> llm, models, providers, auth, Fantasy
llm -> (stdlib only)
tools -> (stdlib only)
session -> llm (types only)
event -> llm (types only)
config -> (stdlib only)
auth -> (stdlib only)
skills -> config
```

`agent` must never import `ui`. `tools` must never import `agent`, `llm`, or `ui`. The agent reports what happens by emitting events; the UI decides how to show them.

## 5. Concurrency model

Deliberately boring:

- The program is effectively **single-threaded** per turn. `agent.Run` is a blocking call. It invokes `emit(Event)` synchronously on the caller's goroutine, so the UI prints in order with no channels or locks.
- Readline only blocks between turns, so input and output never race.
- **Cancellation:** `main` creates a `context.Context` per turn using `signal.NotifyContext(ctx, os.Interrupt)`. Ctrl+C during a turn cancels that context, which stops the LLM stream and kills running subprocesses. Ctrl+C at the prompt exits; Ctrl+D at an empty prompt also exits.
- The only other goroutines: the LLM provider's stream producer and `bash` subprocess output readers. Both must exit when the context is cancelled.

## 6. Package specifications

### 6.1 `internal/event`

The contract between `agent` and `ui`. Plain data.

```go
package event

type Kind int

const (
    TextDelta   Kind = iota // streamed assistant text; Text set
    ToolStart               // model requested a tool; Call set
    ToolResult              // tool finished; Call and Result set
    TurnEnd                 // assistant finished (no more tool calls); Usage set
    Notice                  // informational (e.g. "compacting context"); Text set
    Error                   // recoverable or fatal error; Err set
)

type Event struct {
    Kind   Kind
    Text   string
    Call   *ToolCall
    Result *ToolResult
    Usage  *Usage
    Err    error
}
```

`ToolCall`, `ToolResult`, `Usage` are defined in `llm` and referenced here via type aliases or by placing shared types in `llm`. Pick one approach and keep it simple: **define shared types in `llm`, have `event` import `llm`.** (This is the one allowed exception to the "event has only stdlib deps" line in section 4. Update that line if you take this route.)

### 6.2 `internal/llm`

The provider seam. Our own small interface, with Fantasy hidden behind one adapter file.

```go
package llm

type Role string

const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

type Message struct {
    Role    Role
    Text    string       // user/assistant text
    Calls   []ToolCall   // assistant only
    Results []ToolResult // tool role only
}

type ToolCall struct {
    ID   string
    Name string
    Args json.RawMessage
}

type ToolResult struct {
    CallID  string
    Content string
    IsError bool
}

type ToolSpec struct {
    Name        string
    Description string
    Schema      json.RawMessage // JSON Schema for args
}

type Request struct {
    System   string
    Messages []Message
    Tools    []ToolSpec
}

type Usage struct {
    InputTokens  int
    OutputTokens int
}

// Delta is one item from a streaming response.
type Delta struct {
    Text  string    // incremental text, if any
    Call  *ToolCall // a complete tool call, if any
    Usage *Usage    // set on final delta
    Err   error     // terminal error
}

type Provider interface {
    // Stream starts a request. The channel is closed when the response is
    // complete or ctx is cancelled. A terminal failure is sent as Delta.Err.
    Stream(ctx context.Context, req Request) (<-chan Delta, error)
}
```

**Files:**

- `llm/llm.go`: the types above and the `Provider` interface, nothing else.
- `llm/dummy.go`: the no-model provider.
- `models/models.go`: pigo's `models.json`/`models-store.json` catalog, `ModelEntry`,
  `AvailableModels`, `Find`, `ContextWindow`, and models.json key/header
  interpolation. Both files live in pigo's config dir
  (`$XDG_CONFIG_HOME/pigo`, default `~/.config/pigo`).
- `providers/providers.go`: the provider list, standard env vars, credential
  resolution (`ResolveKey`), and `AuthenticatedModels`.
- `fantasy/fantasy.go`: `NewFantasy`, `NewFantasyEntry`, `NewFromRef`, and
  `BuildProvider`. The only package that imports `charm.land/fantasy`.

**Implementation notes for `fantasy.go`:**

- Fantasy is a **preview** library and its API changes. Before writing the adapter, read the current Fantasy README, `pkg.go.dev/charm.land/fantasy`, and its `examples/` directory. Do not rely on memory of its API.
- Fantasy ships its own multi-step agent. **We do not use it for the loop.** We own the loop (section 6.3) so that compaction, cancellation, and approval stay under our control. Use Fantasy's lower-level language-model streaming API through the adapter. If that proves impractical, the fallback is to run one Fantasy step per `Stream` call, but the `Provider` interface must not change.
- Tool definitions are passed to Fantasy as provider-visible schemas only. Execution stays in our `tools` package.
- Provider selection: `PIGO_PROVIDER` (e.g. `anthropic`, `openai`, `openrouter`) and `PIGO_MODEL` env vars, or `-m provider/model`. API keys resolve in a fixed order: `auth.json` (written by `/login`), then the provider's standard env var, then the `models.json` entry's `apiKey` source (literal, `$ENV`, or `!command`). The `auth` package owns the file and the resolver; `llm` only calls it.
- Isolation rule: if Fantasy is ever replaced, only `internal/fantasy` changes.

### 6.3 `internal/agent`

The loop. This is the heart of the program and should stay under ~150 lines.

```go
package agent

type Agent struct {
    Provider llm.Provider
    Tools    map[string]tools.Tool
    System   string
    Approve  func(llm.ToolCall) bool // nil means always approve
}

// Run appends userText to history, runs the loop until the model stops
// calling tools, and returns the updated history. It emits events as it goes.
func (a *Agent) Run(ctx context.Context, history []llm.Message, userText string,
    emit func(event.Event)) ([]llm.Message, error)
```

**Loop pseudocode:**

```
history += user message
for step := 0; step < MaxSteps; step++ {
    req := Request{System, history, toolSpecs}
    stream := Provider.Stream(ctx, req)
    assistant := Message{Role: assistant}
    for delta in stream {
        if delta.Text   { assistant.Text += delta.Text; emit(TextDelta) }
        if delta.Call   { assistant.Calls += delta.Call; emit(ToolStart) }
        if delta.Err    { emit(Error); return history, err }
        if delta.Usage  { usage = delta.Usage }
    }
    history += assistant
    if len(assistant.Calls) == 0 { emit(TurnEnd); return history, nil }

    results := []ToolResult{}
    for call in assistant.Calls {           // sequential in v1
        if Approve != nil && !Approve(call) { result = denied }
        else { out, err := Tools[call.Name].Run(ctx, call.Args); result = ... }
        emit(ToolResult)
        results += result
    }
    history += Message{Role: tool, Results: results}
}
emit(Error: max steps reached)
```

**Rules:**

- A tool error is **not** a loop error. Convert it to a `ToolResult{IsError: true}` so the model can see it and recover.
- Unknown tool name: return an error result, do not crash.
- On context cancellation mid-turn, return the history as-is plus any completed assistant message. If tool calls were requested but not executed, append synthetic error results (`"cancelled by user"`) so the history stays valid for the provider API (every call needs a result).
- Tool calls run **sequentially** in v1. Parallelism is an optimization to add only if measured to matter.
- `MaxSteps` is a constant in `config`.

### 6.4 `internal/tools`

```go
package tools

type Tool interface {
    Spec() llm.ToolSpec
    Run(ctx context.Context, args json.RawMessage) (string, error)
}
```

(`tools` importing `llm` for `ToolSpec` is acceptable. If this feels like a leak, define a local `Spec` struct and convert in `agent`. Choose one; prefer the local struct if it keeps `tools` stdlib-only.)

Each tool is one file, one struct, one `Run`. Arguments are decoded into a small struct with `json.Unmarshal`. Paths are resolved relative to the working directory. Output returned to the model is always a string.

| Tool | Args | Behavior |
|------|------|----------|
| `read` | `path`, optional `offset` (line), `limit` (lines) | Return file contents with line numbers. Truncate to `MaxReadBytes`; say so explicitly when truncated and how to continue (`offset`). Refuse binary files with a clear message. |
| `write` | `path`, `content` | Create or overwrite. Create parent directories. Return bytes written. |
| `edit` | `path`, `old`, `new` | Exact, literal string replacement. `old` must occur **exactly once**; otherwise return an error explaining zero or multiple matches. No regex, no fuzzy matching. |
| `bash` | `command`, optional `timeout` (seconds) | Run via `bash -c` in the working directory. Capture combined stdout and stderr. Enforce a timeout (default in config). Kill the whole process group on timeout or cancel. Truncate to the **tail** of the output (`MaxBashBytes`) because the end usually has the error. Include exit code in the result. |

**Truncation messages must be explicit** (for example `[output truncated: showing last 20000 of 83211 bytes]`) so the model knows information is missing.

Tool descriptions (the text the model sees) are constants at the top of each file. Keep them short and concrete; they are prompt engineering and deserve care.

### 6.5 `internal/session`

Append-only JSONL. One file per session.

- Location: `$XDG_STATE_HOME/pigo/sessions/<project>/<timestamp>-<shortid>.jsonl`
  (default `~/.local/state/pigo/sessions/`). `<project>` is a filesystem-safe
  name derived from the working directory, so resume only sees the current
  project. `session.ProjectDir()` returns it.
- One line per record:

```json
{"t":"2026-10-08T14:03:11Z","type":"message","id":"a1b2c3","parent_id":"","message":{"role":"user","text":"..."}}
{"t":"2026-10-08T14:03:15Z","type":"message","id":"d4e5f6","parent_id":"a1b2c3","message":{"role":"assistant","text":"...","calls":[...]}}
{"t":"2026-10-08T14:20:00Z","type":"compaction","id":"c0","parent_id":"d4e5f6","summary":"...","first_kept_id":"a1b2c3"}
{"t":"2026-10-08T14:21:00Z","type":"leaf","leaf_id":"a1b2c3"}
{"t":"2026-10-08T14:22:00Z","type":"branch_summary","id":"b0","parent_id":"a1b2c3","from_id":"d4e5f6","summary":"..."}
```

```go
package session

func New(dir string) (*Session, error)
func Open(path string) (*Session, []llm.Message, []string, error) // replay to rebuild the active branch
func Latest(dir string) (string, error)                           // for `pigo -c`
func ProjectDir() string                                          // sessions dir for the working directory
func Resolve(dir, ref string) (string, error)                     // resolve a path or session id within the project
func (s *Session) Append(m llm.Message) (string, error)           // fsync not required; flush each write

// Tree navigation for `/tree`.
type Tree struct { Nodes map[string]*Node; Order []string; Leaf string }
func (s *Session) Tree() (*Tree, error)
func (s *Session) SetLeaf(id string) ([]llm.Message, []string, error)
func (s *Session) AppendBranchSummary(fromID, summary string) error
func (t *Tree) Entries() []Entry
func (t *Tree) Abandoned(fromLeaf, toLeaf string) []llm.Message
```

Entries link via `id`/`parent_id`, forming a tree rather than a line. The
active branch is the path from `Leaf` to the root; appending continues from the
leaf. Legacy records without ids or parents are chained in file order, so old
sessions stay readable.

**Rules:**

- Never rewrite or truncate a session file. Compaction appends a `compaction` record; on replay, messages before the record are replaced by the summary. Navigation appends a `leaf` record; branch summaries append a `branch_summary` record.
- Writes happen after each completed message (user, assistant, tool results), not per delta.
- A corrupt trailing line (crash mid-write) is skipped with a warning on load, not fatal.

### 6.6 Context management and compaction

- Track `InputTokens` from the last `Usage`. When it exceeds `CompactAtTokens` (config), compact **before the next request**.
- Compaction = one extra LLM call (no tools) asking the model to summarize the older part of the conversation. Keep the most recent N messages verbatim. Replace the older part with a single user message containing the summary. Append the `compaction` record to the session.
- Emit a `Notice` event when compaction runs.
- **Milestone:** compaction is not required for the first working version. Build it after the core loop is solid (section 13).

### 6.7 `internal/ui`

All terminal concerns live here and only here.

**Input:** a readline library (`github.com/chzyer/readline` or `github.com/reeflective/readline`; pick one and hide it behind `ui.ReadLine(prompt) (string, error)`). Requirements: history (persisted to a file in the state dir), Ctrl+C clears the line, Ctrl+D exits on empty input, multi-line input via a trailing backslash or a `"""` fence. The primary prompt is built by the UI: the model label sits before the `>` marker, which turns yellow while the line starts with `!` and red after a failed turn.

**Output:** `ui.Render(event.Event)` is the single entry point that the agent's `emit` callback calls.

| Event | Rendering |
|-------|-----------|
| `TextDelta` | Streamed to stdout (see below). |
| `ToolStart` | One dim line: `> bash: go test ./...` (tool name plus a short arg summary, truncated to terminal width). |
| `ToolResult` | Dim, indented, first N lines of output (`UIToolLines` config), with `... (+M lines)` if cut. Errors in red. Full output still goes to the model. |
| `TurnEnd` | Blank line; optional dim token usage footer. |
| `Notice` | Dim italic line. |
| `Error` | Red line to **stderr**. |

**Streaming markdown with glamour.** Glamour renders whole documents, not fragments. Strategy: **block buffering.**

1. Accumulate streamed text in a buffer.
2. A *block* completes at a blank line **outside** a code fence, or when a code fence closes.
3. Render each completed block with glamour and print it. At `TurnEnd`, flush the remainder.
4. Config flag `RenderMode`: `"block"` (default) or `"raw"` (print deltas as they arrive, no markdown rendering). `raw` is also used automatically when stdout is not a TTY.

**TTY handling:**

- If stdout is not a terminal (piped), emit no ANSI styling and no glamour; print plain text. This makes `pigo -p "..." | tee out.txt` clean.
- Respect `NO_COLOR`.
- Get terminal width for glamour word wrap; fall back to 80.

**Slash commands** (handled in `main`/`ui`, not sent to the model). Keep this list tiny:

| Command | Action |
|---------|--------|
| `/exit` | Quit |
| `/new` | Start a fresh session |
| `/resume` | Pick another session in the current project; clears the screen and restores it |
| `/compact` | Force compaction (once implemented) |
| `/tree [n\|id]` | Show the session tree and switch the active branch to a chosen entry. With an argument, switch directly (1-based number or id prefix). The abandoned branch is summarized on request. |
| `/model` | Open the interactive model picker (Ctrl+L). With an argument, switch directly. |
| `/login` | List providers with a key-status marker and store an API key for the selected one. `/login <provider>` skips the list. OAuth/subscription providers report that they are unsupported. |
| `/logout` | List providers with a stored key and remove the selected one. |

The provider and model pickers share one selector. On a terminal narrower than a
small threshold it falls back to a numbered prompt instead of the raw arrow-key
list.

The model picker lists `provider/model` refs from pigo's `models.json`
(`$XDG_CONFIG_HOME/pigo/models.json`, default `~/.config/pigo/models.json`) and
its cached provider catalog `models-store.json`, merged with the built-in
defaults and filtered to providers with a usable key (auth.json, the provider
env var, or a `models.json` `apiKey`). It is an in-place raw-terminal list:
type to filter, Up/Down to move, Enter to select, Esc to cancel. The list is rebuilt
when `/model` opens and after `/login`, so a new credential shows its models.
The selected ref is remembered in `$XDG_STATE_HOME/pigo/model` (default
`~/.local/state/pigo/model`) and restored on the next start, unless `-m`,
`PIGO_PROVIDER`, or `PIGO_MODEL` overrides it.

### 6.8 `internal/skills`

Discovers Agent Skills and advertises them in the system prompt. A skill is a
directory containing a `SKILL.md` whose leading frontmatter declares a `name`
and `description`.

```go
type Skill struct {
    Name         string
    Description  string
    Path         string // absolute path to SKILL.md
    DisableModel bool   // disable-model-invocation: true
}

func Discover() []Skill      // global then project; nearer project wins by name
func Section([]Skill) string // system-prompt section, or ""
```

- Global skills live in `ConfigDir()/skills` (default `~/.config/pigo/skills`).
- Project skills live in `.pigo/skills` in the working directory and each parent
  up to the home directory, discovered recursively.
- The frontmatter parser is line-based, not a YAML dependency. A file without
  both `name` and `description`, or without a valid leading `---` fence, is not
  loaded.
- `Section` lists each model-visible skill as `name: description (read path)` and
  tells the model to read the `SKILL.md` before following it. Skills with
  `disable-model-invocation: true` are omitted from the prompt but still
  discovered.

### 6.9 `internal/config`

One file, `config.go`, mostly constants. Edit and rebuild to change.

```go
package config

const (
    MaxSteps        = 50
    MaxReadBytes    = 50_000
    MaxBashBytes    = 20_000
    BashTimeoutSecs = 120
    CompactAtTokens = 120_000
    KeepRecentMsgs  = 8
    UIToolLines     = 8
    RenderMode      = "block" // "block" or "raw"
    DefaultProvider = "anthropic"
    DefaultModel    = "" // empty: require PIGO_MODEL or -m
)

const SystemPrompt = `...` // the base system prompt, a plain string constant
```

The context-file helpers live here too (section 7): `ContextFiles()` returns the
loaded files with their paths and global/project scope, and `PromptWithFiles`
builds the prompt from them. `PromptWithContext` is the shorthand for
`PromptWithFiles(ContextFiles())`.

Runtime inputs (not constants): `PIGO_PROVIDER`, `PIGO_MODEL`, provider API-key env vars, and flags.

### 6.10 `cmd/pigo/main.go`

Wiring only: parse flags, build provider, tools, agent, session, then run either the interactive loop or the one-shot path. Target: under 150 lines. No business logic here.

**Flags:**

| Flag | Meaning |
|------|---------|
| `-p "text"` | Non-interactive: run one turn with this prompt (or read stdin if `-p -`), print the result, exit. |
| `-c` | Continue the most recent session in the current project. |
| `-r path\|id` | Resume a session by file path or project session id. |
| `-m provider/model` | Override model. |
| `-confirm` | Ask before running each `bash` call (sets `Agent.Approve`). |

**Interactive loop:**

```
load session or create new
for {
    line, err := ui.ReadLine("> ")
    if EOF: break
    if slash command: handle; continue
    ctx := signal.NotifyContext(...)
    history, err = agent.Run(ctx, history, line, ui.Render)
    session.Append(new messages)
}
```

**Exit codes:** `0` success, `1` runtime error, `2` usage error. In `-p` mode, a failed turn exits non-zero.

## 7. Project context file

On startup, `config.PromptWithContext` builds the system prompt from two layers:

1. **Global:** `AGENTS.md` (then `CLAUDE.md`) in pigo's config directory,
   `$XDG_CONFIG_HOME/pigo` (default `~/.config/pigo`). These are user
   instructions applied in every working directory.
2. **Project:** `AGENTS.md` (then `CLAUDE.md`) in the working directory and each
   parent up to the home directory or filesystem root, outermost first.

In any directory, `AGENTS.override.md` replaces `AGENTS.md` and `CLAUDE.md`
from that same directory only; it does not suppress context from other
directories.

Contents are appended under `# Global context` and `# Project context`
headings; the nearer project file is last, so it takes precedence. A file in
the global directory is not loaded twice when pigo runs inside it. This is the
only "magic" context file. No other context discovery mechanisms.

Skills are discovered separately (section 6.8) and appended as a `# Skills`
section: global `$XDG_CONFIG_HOME/pigo/skills` and project `.pigo/skills`. Only
names, descriptions, and paths are loaded up front; the model reads `SKILL.md`
on demand via the `read` tool.

**Startup notice.** In interactive mode `main` prints one dim `Notice` line per
loaded context file and skill, each labelled `global` or `project` with its
path, so the user can see what was picked up. Non-interactive (`-p`) runs stay
silent.

## 8. Repository layout

```
pigo/
  cmd/pigo/main.go  auth.go
  internal/
    agent/     agent.go  agent_test.go
    auth/      auth.go  auth_test.go
    config/    config.go
    diff/      diff.go  diff_test.go
    event/     event.go  json.go
    fantasy/   fantasy.go  fantasy_test.go  attribution_test.go
    llm/       llm.go  dummy.go
    markdown/  markdown.go  markdown_test.go
    models/    models.go  models_test.go  models_available_test.go  store_test.go
    providers/ providers.go  keys_test.go  store_test.go
    selector/  selector.go  selector_test.go
    session/   session.go  session_test.go
    skills/    skills.go  skills_test.go
    tools/     tools.go  read.go  write.go  edit.go  bash.go  *_test.go
    ui/        ui.go  tool.go  style.go  readline.go  commands.go  selector.go
  go.mod
  Makefile          # build, test, vet, fmt, line-count
  ARCHITECTURE.md   # this file
  AGENTS.md         # short pointer: "read ARCHITECTURE.md; obey section 11"
```

## 9. Dependencies

Allowed:

| Dependency | Used for | Where |
|-----------|----------|-------|
| `charm.land/fantasy` (+ chosen provider subpackages) | Provider-agnostic model access | `internal/fantasy` only |
| A readline library | Line editing and history | `ui/readline.go` only |
| `github.com/charmbracelet/lipgloss` | Styling | `ui` only |
| `github.com/charmbracelet/glamour` | Markdown rendering | `markdown` only |
| `github.com/charmbracelet/x/ansi` | Display-width truncation for the model picker | `selector` only |
| `golang.org/x/term` | Raw-mode terminal handling for the model picker | `selector` only |
| `golang.org/x/sys/unix` | `poll(2)` for distinguishing Esc from arrow keys | `selector` only |
| Go standard library | Everything else | everywhere |

Anything else needs written justification. In particular: no CLI framework (use `flag`), no logging framework (use `fmt.Fprintf(os.Stderr, ...)`), no assertion/test libraries (use `testing`), no DI containers.

## 10. Extension points (by editing source, not plugins)

| To add... | Do this |
|-----------|---------|
| A tool | Add `internal/tools/foo.go` implementing `Tool`; register it in the tool map in `main.go`. |
| A provider | Usually nothing; Fantasy handles it. Otherwise implement `llm.Provider` in a new file. |
| A slash command | Add a case in the command switch in `main`. |
| A UI behavior | Edit `ui`. The agent is unaffected. |
| Approval policy | Replace the `Agent.Approve` function. |

If a requested feature cannot be added this way without touching three or more packages, stop and reconsider the design with the maintainer.

## 11. Rules for LLM agents working on this repo

1. **Read before writing.** Read this document and the files you will touch.
2. **Respect the dependency direction** in section 4. If your change needs a forbidden import, the design is wrong; ask.
3. **Respect the non-goals** in section 2. Do not add features that are not requested.
4. **Stay within the line budget** (section 12). Report the line count with your change.
5. **No new dependencies** without written justification.
6. **No speculative abstraction.** Do not introduce interfaces, options structs, or config knobs "for flexibility."
7. **Do not modify the `llm.Provider`, `tools.Tool`, or `event.Event` contracts** without explicit instruction. They are the seams everything else depends on.
8. **Verify Fantasy's API** against its current docs and examples; do not guess method names.
9. **Write tests** for `edit` (zero, one, many matches), `session` (replay, corrupt trailing line, compaction record), `agent` (using a fake `Provider`: no tools, one tool call, tool error, cancellation), and block-buffering in `ui/markdown.go`.
10. **Run `gofmt`, `go vet`, and `go test ./...`** before declaring done.
11. **Prefer deleting code** to adding it when both solve the problem.
12. **Comments explain why, not what.** Keep them short. Exported identifiers get a one-line doc comment.
13. **When unsure, ask** rather than choosing a complex option.

## 12. Line budget (non-test Go, excluding blank lines and comments)

| Package | Target | Hard cap |
|---------|--------|----------|
| `cmd/pigo` | 120 | 180 |
| `agent` | 130 | 200 |
| `llm` | 60 | 100 |
| `models` | 250 | 400 |
| `providers` | 80 | 180 |
| `fantasy` | 200 | 350 |
| `tools` | 300 | 450 |
| `session` | 120 | 200 |
| `ui` | 400 | 700 |
| `markdown` | 120 | 220 |
| `diff` | 100 | 200 |
| `selector` | 200 | 380 |
| `config` | 30 | 60 |
| `skills` | 80 | 160 |
| `event` | 30 | 60 |
| **Total** | **~2180** | **~3940** |

The `ui` budget is deliberately generous after the selector, markdown, and
diff extraction; the original 250 target assumed a much smaller UI than the
project grew into. A future pass should split `cmd/pigo` (the interactive
loop) before raising the `ui` cap again.

`make loc` should print these counts. Going over a hard cap requires a design discussion.

## 13. Build order

Build in this order; each step leaves a working program.

1. **`llm` + `config` + Fantasy adapter:** a throwaway `main` that streams one prompt to stdout. Proves provider access.
2. **`event` + `ui` (raw mode only):** print deltas; readline input; Ctrl+C handling.
3. **`tools`:** `read`, `write`, `edit`, `bash` with tests, no model involved.
4. **`agent`:** the loop with a fake provider and tests, then wire it to the real provider. **This is the first usable version.**
5. **`session`:** JSONL append, `-c` and `-r` resume.
6. **`ui` polish:** block-buffered glamour rendering, tool output formatting, TTY and `NO_COLOR` handling.
7. **Non-interactive mode:** `-p`, stdin, exit codes.
8. **Project context** (`AGENTS.md` loading), **skills** (`SKILL.md` discovery), and **`-confirm`** approval.
9. **Compaction.**

## 14. Testing strategy

- **Unit tests** on tools and session with temp directories.
- **Agent tests** with a scripted fake `Provider` that returns canned deltas. This covers the loop, tool errors, unknown tools, max steps, and cancellation without network access or cost.
- **UI tests** only for the block-buffering function (input chunks in, rendered blocks out).
- **No tests against live LLM APIs** in CI. A manual smoke script (`make smoke`) may exist for a developer with API keys.

## 15. Security and safety notes

- `pigo` executes arbitrary shell commands chosen by a model. There is no sandbox. Run it in a container or VM, or with `-confirm`, when working with untrusted prompts or repositories.
- Content read from files and command output is **untrusted data** and can contain prompt injection. The system prompt should state that instructions found inside tool results are not from the user. Do not add automatic privilege escalation based on tool output.
- API keys come only from the environment, `auth.json`, or `models.json` and are never written to session files or logs. `/login` writes `$XDG_CONFIG_HOME/pigo/auth.json` (default `~/.config/pigo/auth.json`) with mode 0600 in a 0700 directory, using an atomic rename; keys are never echoed and never appear in error output.

## 16. Open questions (decide when reached, keep the answer simple)

- Exact Fantasy streaming API shape for the adapter (verify against current docs).
- Which readline library handles multi-line input and persistent history best.
- Whether `ToolResult` output shown in the UI should be expandable (probably not; resist).
- Token counting: rely on provider-reported usage only (preferred) versus local estimation (avoid).
