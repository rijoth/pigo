# pigo

A minimal terminal coding agent written in Go, in the spirit of Pi.

pigo runs one simple loop: send your conversation to an LLM, execute the tool
calls the model asks for, feed the results back, and repeat until the model
answers without calling a tool. It prints to normal stdout, so scrollback and
copy/paste work the way you expect. It is not a fullscreen TUI.

```
> refactor the retry logic in client.go and run the tests
```

The model gets four tools: `read`, `write`, `edit`, and `bash`. That turns out
to be enough for most software work.

## Why another coding agent

- **Small and readable.** A competent developer can read the whole codebase in
  about an hour. Roughly 13k lines including tests.
- **Provider agnostic.** Anthropic, OpenAI, Google, OpenRouter, and many more
  are a config change. Swapping models or providers never touches the loop.
- **Compiles to a single binary.** No runtime, no node_modules, no daemon.
- **Sessions are plain JSONL.** You can resume them, inspect them with `jq`,
  and diff them.
- **Unix friendly.** Pipes and redirects behave. `pigo -p "..." | tee out.txt`
  prints clean text with no ANSI noise.
- **Deterministic compaction.** When context fills up, older history is
  summarized locally with no extra model call.

## Requirements

- Go 1.26 or newer to build.
- An API key for at least one provider.
- Linux. macOS should work. Windows is untested.

## Build

```sh
make build
```

That writes a `pigo` binary in the repo root. To install it somewhere on your
`PATH`:

```sh
go build -o ~/.local/bin/pigo ./cmd/pigo
```

Other make targets:

```sh
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
make loc     # line counts per package
make smoke   # one live prompt, needs a configured key
```

## First run

Set a key and pick a model.

```sh
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/pigo -m anthropic/claude-sonnet-4-5
```

Or store the key once with `/login` inside the agent, then start it with just:

```sh
./pigo
```

`/login` writes `~/.config/pigo/auth.json` with mode `0600` inside a `0700`
directory, using an atomic rename. Keys are never echoed and never written to
session files or logs.

The model you pick is remembered in `~/.local/state/pigo/model`, so the next
start brings it back. `-m`, `PIGO_PROVIDER`, and `PIGO_MODEL` override it.

## Usage

### Interactive

```sh
pigo
```

Type a message and press Enter. Ctrl+C while the model is working cancels the
turn and kills any running subprocess. Ctrl+C at an empty prompt or Ctrl+D
exits. Multi-line input works with a trailing backslash or a `"""` fence.

### One shot

```sh
pigo -p "explain what this repo does"
cat bug.log | pigo -p -
```

`-p` runs a single turn, prints the result, and exits. Output goes to stdout
with no styling when it is not a terminal, so it composes well in scripts.

### Resuming

```sh
pigo -c              # continue the most recent session in this directory
pigo -r <id|path>    # resume a specific session
```

Sessions are scoped per project, so `-c` only sees sessions started from the
current working directory.

### Flags

| Flag | Meaning |
|------|---------|
| `-p "text"` | Run one prompt non-interactively. Use `-p -` to read stdin. |
| `-c` | Continue the most recent session in the current project. |
| `-r path\|id` | Resume a session by file path or project session id. |
| `-m provider/model` | Override the model for this run. |
| `-confirm` | Ask before running each `bash` call. |
| `-json` | Emit one JSON event per line instead of styled text. Requires `-p`. |

## Slash commands

These are handled by pigo itself and are never sent to the model.

| Command | Action |
|---------|--------|
| `/exit` | Quit. |
| `/new` | Start a fresh session. |
| `/resume` | Pick another session in this project. |
| `/compact [keep:N] [prompt]` | Force compaction now. `keep:N` keeps N recent user turns verbatim; any other text is sent as a follow-up prompt afterward. |
| `/recall [query]` | Search this session, including history that compaction dropped. |
| `/tree [n\|id]` | Show the session tree and switch the active branch. |
| `/model` | Open the model picker. Ctrl+L does the same. An argument switches directly. |
| `/thinking [level]` | Set the reasoning level. Shift+Tab cycles it. Ctrl+T toggles whether reasoning renders expanded. |
| `/login` | Store an API key for a provider. |
| `/logout` | Remove a stored key. |

## The tools

| Tool | Arguments | What it does |
|------|-----------|--------------|
| `read` | `path`, optional `offset`, `limit` | Returns file contents with line numbers. Refuses binary files. Says explicitly when output was truncated and how to continue. |
| `write` | `path`, `content` | Creates or overwrites a file, making parent directories as needed. |
| `edit` | `path`, `old`, `new` | Exact literal replacement. `old` must appear exactly once, otherwise it errors. No regex, no fuzzy matching. |
| `bash` | `command`, optional `timeout` | Runs `bash -c` in the working directory, captures combined stdout and stderr, enforces a timeout, and kills the whole process group on cancel. |
| `pigo_recall` | `query`, `ref`, `mode`, `scope`, `limit`, `page` | Searches this session's history, including anything compaction dropped. |

## Sessions and compaction

One JSONL file per session, one line per record:

```
~/.local/state/pigo/sessions/<project>/<timestamp>-<shortid>.jsonl
```

Records link by `id` and `parent_id`, so history is a tree rather than a line.
`/tree` walks it and can switch branches; the abandoned branch gets summarized
on request. Files are append only. Nothing is ever rewritten or truncated.

When the context window fills up, pigo compacts. Compaction is deterministic:
older history is normalized, noise is filtered, structured sections are
extracted (session goal, files and changes, commits, outstanding context, and
so on), and a ranked brief replaces the older messages. No extra model call,
no network. `[Earlier conversation summary]` marks the boundary and the raw
summary is appended to the session.

## Model and provider configuration

pigo reads `models.json` from `~/.config/pigo/models.json` and a cached
`models-store.json`, falling back to Pi's agent directory
(`PI_CODING_AGENT_DIR`, default `~/.pi/agent`) when pigo has not fetched the
catalog itself. Built in defaults are merged in.

Keys resolve in a fixed order:

1. `~/.config/pigo/auth.json`, written by `/login`.
2. The provider's standard environment variable, for example
   `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`.
3. The `apiKey` field in a `models.json` entry, which can be a literal, `$ENV`,
   or `!command`.

Provider and model can also be set with `PIGO_PROVIDER` and `PIGO_MODEL`.

## Project context and skills

On startup pigo builds its system prompt from context files.

1. **Global:** `AGENTS.md` (then `CLAUDE.md`) in `~/.config/pigo`.
2. **Project:** `AGENTS.md` (then `CLAUDE.md`) in the working directory and each
   parent up to your home directory, outermost first. The nearest file wins.
   `AGENTS.override.md` replaces `AGENTS.md` and `CLAUDE.md` from the same
   directory only.

Agent Skills are discovered from `~/.config/pigo/skills` and `.pigo/skills` in
the project. A skill is a directory containing a `SKILL.md` with `name` and
`description` frontmatter. Only names, descriptions, and paths go into the
prompt; the model reads the full file on demand. Set
`disable-model-invocation: true` to discover a skill without advertising it.

In interactive mode pigo prints one dim notice per loaded context file and
skill so you can see what was picked up. `-p` runs stay silent.

## Configuration

There is no config file parser. Defaults are constants in
`internal/config/config.go`, in the spirit of a `config.h`. Edit and rebuild to
change them. The ones you are most likely to touch:

| Constant | Default | Meaning |
|----------|---------|---------|
| `MaxSteps` | 50 | Tool-call rounds before a turn gives up. |
| `MaxReadBytes` | 50000 | Read truncation limit. |
| `MaxBashBytes` | 20000 | Bash output cap, keeping the tail. |
| `BashTimeoutSecs` | 120 | Default bash timeout. |
| `CompactAtTokens` | 120000 | Compact once usage passes this. |
| `UIToolLines` | 8 | Tool output lines shown before `... (+M lines)`. |
| `Theme` | `"dark"` | Glamour/chroma style, `dark` or `light`. |
| `RenderMode` | `"block"` | `block` renders markdown per block, `raw` streams plain deltas. |
| `DefaultThinking` | `"medium"` | Startup reasoning level. |

Only secrets and per-run choices come from the environment and flags.

## Security

pigo runs shell commands that a model chooses. There is no sandbox. Treat it
like handing an agent an interactive shell:

- Run it in a container or VM, or with `-confirm`, when the prompt or the
  repository is untrusted.
- File contents and command output are untrusted data and can contain prompt
  injection. The system prompt tells the model not to follow instructions found
  inside tool results, but that is a mitigation and not a guarantee.
- API keys live only in `auth.json`, the environment, or `models.json`, and are
  never written to sessions.

## Layout

```
cmd/pigo/        flags, wiring, interactive loop
internal/
  agent/         the loop
  llm/           provider interface and message types
  fantasy/       the only package that imports charm.land/fantasy
  tools/         read, write, edit, bash, recall
  session/       JSONL store, tree, recall
  compact/       deterministic context compaction
  ui/            readline, rendering, slash commands, selectors
  markdown/      glamour block rendering
  models/        model catalog and reasoning levels
  providers/     provider list and credential resolution
  auth/          auth.json
  config/        constants and context files
  skills/        SKILL.md discovery
  mention/       @path expansion
  diff/          unified diff previews
  selector/      terminal picker
```

The design and the hard rules for contributors live in `ARCH.md`. Read it
before changing code, and see section 11 in particular.

## Development

```sh
make test
make vet
gofmt -l .
```

Tests do not call live models. The agent loop is exercised with a scripted fake
provider, so the suite runs offline and costs nothing.

## Status

pigo is a personal project under active development. The design is stable and
the tool is usable daily, but preview dependencies may change and the line
budgets in `ARCH.md` are enforced by hand rather than by CI.
