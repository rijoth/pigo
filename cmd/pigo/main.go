// Command pigo is a minimal terminal coding agent.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pigo/internal/agent"
	"pigo/internal/auth"
	"pigo/internal/config"
	"pigo/internal/event"
	"pigo/internal/fantasy"
	"pigo/internal/llm"
	"pigo/internal/models"
	"pigo/internal/providers"
	"pigo/internal/session"
	"pigo/internal/skills"
	"pigo/internal/tools"
	"pigo/internal/ui"
)

func main() {
	os.Exit(run())
}

func run() int {
	prompt := flag.String("p", "", "run one prompt non-interactively (use - to read stdin)")
	cont := flag.Bool("c", false, "continue the most recent session")
	resume := flag.String("r", "", "resume a session by file path or project id")
	modelFlag := flag.String("m", "", "model as provider/model")
	confirm := flag.Bool("confirm", false, "ask before running each bash call")
	jsonOut := flag.Bool("json", false, "write one JSON event per line (requires -p)")
	flag.Parse()
	if *jsonOut && *prompt == "" {
		fmt.Fprintln(os.Stderr, "pigo: -json requires -p")
		return 2
	}

	state := session.StateDir()
	projectDir := session.ProjectDir()
	resumePath := ""
	if *resume != "" {
		resolved, rerr := session.Resolve(projectDir, *resume)
		if rerr != nil {
			return fatal(rerr)
		}
		resumePath = resolved
	}
	sess, history, ids, err := session.OpenOrNew(resumePath, *cont, projectDir)
	if err != nil {
		return fatal(err)
	}
	defer sess.Close()
	resumed := resumePath != "" || *cont

	active := sess
	sessionID := func() string { return active.ID() }
	var provider llm.Provider
	var modelLabel string
	if *modelFlag == "" && os.Getenv("PIGO_PROVIDER") == "" && os.Getenv("PIGO_MODEL") == "" {
		if ref := session.LoadModelRef(); ref != "" {
			if p, label, found, buildErr := fantasy.NewFromRef(ref, sessionID); buildErr == nil && found {
				provider, modelLabel = p, label
			}
		}
	}
	if provider == nil {
		provider, modelLabel, err = fantasy.BuildProvider(*modelFlag, config.DefaultProvider, sessionID)
		if err != nil {
			return fatal(err)
		}
	}

	u := ui.New(os.Stdout, os.Stderr, filepath.Join(state, "history"))
	defer u.Close()
	if *prompt != "" {
		u.DisableSpinner() // -p: keep stderr quiet for scripts
	}
	u.SetModels(pickerRefs())
	u.SetContextWindow(models.ContextWindow(modelLabel))
	u.SetModel(modelLabel)
	if wd, werr := os.Getwd(); werr == nil {
		u.SetProject(config.RelHome(wd))
	}

	contexts := config.ContextFiles()
	loaded := skills.Discover()
	system := config.PromptWithFiles(contexts)
	if s := skills.Section(loaded); s != "" {
		system += "\n\n" + s
	}
	banner := func(model string) string { return bannerText(contexts, loaded, model) }
	if u.Interactive() {
		if *prompt == "" && resumed {
			u.Restore(banner(modelLabel), history)
		} else {
			u.Banner(banner(modelLabel))
		}
	}

	thinking := startupThinking(modelLabel)
	u.SetThinkingLevel(thinking)
	ag := &agent.Agent{
		Provider:   provider,
		Tools:      tools.Default(),
		System:     system,
		Thinking:   thinking,
		MaxSteps:   config.MaxSteps,
		MaxRetries: config.MaxRetries,
		RetryBase:  time.Duration(config.RetryBaseMs) * time.Millisecond,
		RetryMax:   time.Duration(config.RetryMaxMs) * time.Millisecond,
	}
	if *confirm {
		ag.Approve = func(call llm.ToolCall) bool {
			u.StopSpinner()
			ok := u.Confirm(call.Name, call.Args)
			u.StartSpinner()
			return ok
		}
	}

	if *prompt != "" {
		if *jsonOut {
			emit := func(ev event.Event) { _ = event.Encode(os.Stdout, ev) }
			return runOnce(ag, sess, history, *prompt, emit, func() {})
		}
		return runOnce(ag, sess, history, *prompt, u.Render, u.Interrupt)
	}
	return runInteractive(ag, sess, history, ids, u, modelLabel, banner, projectDir, sessionID, func(s *session.Session) { active = s })
}

func fatal(err error) int {
	fmt.Fprintln(os.Stderr, "pigo:", err)
	return 1
}

// supportedThinking returns the reasoning levels a model ref supports; an
// unknown model supports only "off".
func supportedThinking(ref string) []string {
	if e, ok := models.Find(models.AvailableModels(), ref); ok {
		return e.SupportedThinkingLevels()
	}
	return []string{"off"}
}

// hasLevel reports whether level is in levels.
func hasLevel(levels []string, level string) bool {
	for _, l := range levels {
		if l == level {
			return true
		}
	}
	return false
}

// clampThinking returns level clamped to what the model supports.
func clampThinking(ref, level string) string {
	if e, ok := models.Find(models.AvailableModels(), ref); ok {
		return e.ClampThinkingLevel(level)
	}
	return "off"
}

// startupThinking returns the remembered reasoning level, or the configured
// default, clamped to the model's supported levels.
func startupThinking(ref string) string {
	level := session.LoadThinkingLevel()
	if level == "" {
		level = config.DefaultThinking
	}
	return clampThinking(ref, level)
}

// bannerText renders the startup summary: pigo version, the context files and
// skills picked up, and the model, using paths relative to the working
// directory or the home directory.
func bannerText(contexts []config.ContextFile, loaded []skills.Skill, modelLabel string) string {
	var paths, names []string
	for _, f := range contexts {
		paths = append(paths, config.RelPath(f.Path))
	}
	for _, s := range loaded {
		names = append(names, s.Name)
	}
	var b strings.Builder
	b.WriteString("PiGo " + fantasy.Version)
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("[" + title + "]\n" + strings.Join(items, ", "))
	}
	section("Context", paths)
	section("Skills", names)
	if modelLabel != "" {
		section("Model", []string{modelLabel})
	}
	return b.String()
}

func runOnce(ag *agent.Agent, sess *session.Session, history []llm.Message, prompt string, emit func(event.Event), interrupt func()) int {
	if prompt == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fatal(err)
		}
		prompt = string(b)
	}
	if strings.TrimSpace(prompt) == "" {
		fmt.Fprintln(os.Stderr, "pigo: empty prompt")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	n := len(history)
	history, err := ag.Run(ctx, history, prompt, emit)
	if ctx.Err() != nil {
		interrupt()
	}
	if _, err := appendAll(sess, history[n:]); err != nil {
		return fatal(err)
	}
	if err != nil {
		return 1
	}
	return 0
}

func runInteractive(ag *agent.Agent, sess *session.Session, history []llm.Message, ids []string, u *ui.UI, modelLabel string, banner func(string) string, projectDir string, sessionID func() string, setSession func(*session.Session)) int {
	var lastInput int
	contextWindow := models.ContextWindow(modelLabel)
	compactAt := compactLimit(contextWindow)
	levels := supportedThinking(modelLabel)
	thinking := ag.Thinking
	u.OnCycleThinking(func() string {
		thinking = models.NextThinkingLevel(levels, thinking)
		ag.Thinking = thinking
		session.SaveThinkingLevel(thinking)
		u.SetThinkingLevel(thinking)
		return thinking
	})
	emit := func(ev event.Event) {
		u.Render(ev)
		if ev.Kind == event.TurnEnd && ev.Usage != nil {
			lastInput = ev.Usage.InputTokens
		}
		// Restart once a tool finishes; the model is thinking again.
		switch ev.Kind {
		case event.ToolStart:
			u.StartSpinner()
		case event.ToolResult, event.Notice:
			u.StartSpinner()
		}
	}
	// switchModel resolves ref against models.json plus built-ins and points
	// the agent at it. It reports failures as UI errors without changing state.
	switchModel := func(ref string) {
		p, label, found, err := fantasy.NewFromRef(ref, sessionID)
		if err != nil {
			u.Render(event.Event{Kind: event.Error, Err: err})
			return
		}
		if !found {
			u.Render(event.Event{Kind: event.Error, Err: fmt.Errorf("unknown model %q", ref)})
			return
		}
		ag.Provider = p
		modelLabel = label
		session.SaveModelRef(label)
		contextWindow = models.ContextWindow(label)
		compactAt = compactLimit(contextWindow)
		levels = supportedThinking(label)
		thinking = clampThinking(label, thinking)
		ag.Thinking = thinking
		u.SetContextWindow(contextWindow)
		u.SetModel(label)
		u.SetThinkingLevel(thinking)
		u.Render(event.Event{Kind: event.Notice, Text: "model: " + label})
	}
	for {
		line, err := u.ReadLine(ui.MainPrompt)
		if errors.Is(err, ui.ErrInterrupt) {
			fmt.Fprintln(os.Stdout)
			return 0
		}
		if errors.Is(err, ui.ErrModelSelect) {
			u.SetModels(pickerRefs())
			if ref, selErr := u.SelectModel(modelLabel); selErr == nil {
				switchModel(ref)
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stdout)
			return 0
		}
		if err != nil {
			return fatal(err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if cmd, mode := ui.ParseBang(line); mode != ui.BangNone {
			if cmd == "" {
				fmt.Fprintln(os.Stdout, "usage: !<cmd> adds output to context; !!<cmd> runs it without")
				continue
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			out, code, cmdErr := tools.RunShell(ctx, cmd, 0)
			stop()
			if cmdErr != nil {
				u.Render(event.Event{Kind: event.Error, Err: cmdErr})
			}
			if out != "" {
				u.Render(event.Event{Kind: event.ToolResult, Result: &llm.ToolResult{Content: out}})
			}
			if mode == ui.BangRunAndAdd {
				msg := llm.Message{Role: llm.RoleUser, Text: fmt.Sprintf("I ran `%s` (exit %d):\n%s", cmd, code, out)}
				id, err := sess.Append(msg)
				if err != nil {
					return fatal(err)
				}
				ids = append(ids, id)
				history = append(history, msg)
			}
			continue
		}
		force := false
		compactArgs := ""
		if cmd, ok := strings.CutPrefix(line, "/"); ok {
			name, arg, _ := strings.Cut(strings.TrimSpace(cmd), " ")
			switch name {
			case "exit", "quit":
				return 0
			case "model":
				arg = strings.TrimSpace(arg)
				if arg == "" {
					if !u.Interactive() {
						printModels(os.Stdout, modelLabel, pickerModels())
						continue
					}
					u.SetModels(pickerRefs())
					ref, err := u.SelectModel(modelLabel)
					if errors.Is(err, ui.ErrCancel) {
						continue
					}
					if err != nil {
						u.Render(event.Event{Kind: event.Error, Err: err})
						continue
					}
					arg = ref
				}
				switchModel(arg)
				continue
			case "login":
				handleLogin(u, strings.TrimSpace(arg), auth.Path(), func(provider string) {
					u.SetModels(pickerRefs())
					if provider == providerOf(modelLabel) {
						switchModel(modelLabel)
					}
				})
				continue
			case "thinking":
				arg = strings.ToLower(strings.TrimSpace(arg))
				if arg != "" && !hasLevel(levels, arg) {
					u.Render(event.Event{Kind: event.Error, Err: fmt.Errorf("thinking %q not supported; use %s", arg, strings.Join(levels, ", "))})
					continue
				}
				if arg != "" {
					thinking = arg
				}
				ag.Thinking = thinking
				session.SaveThinkingLevel(thinking)
				u.SetThinkingLevel(thinking)
				u.Render(event.Event{Kind: event.Notice, Text: "thinking: " + thinking + " (Shift+Tab cycles; available: " + strings.Join(levels, ", ") + ")"})
				continue
			case "expand":
				u.Expand()
				continue
			case "logout":
				handleLogout(u, auth.Path(), func() { u.SetModels(pickerRefs()) })
				continue
			case "resume":
				if resumeSession(u, &sess, &history, &ids, &lastInput, banner(modelLabel), projectDir, setSession) {
					contextWindow = models.ContextWindow(modelLabel)
					compactAt = compactLimit(contextWindow)
					u.SetContextWindow(contextWindow)
				}
				continue
			case "tree":
				navigateTree(ag, sess, u, &history, &ids, &lastInput, banner(modelLabel), strings.TrimSpace(arg))
				continue
			case "session":
				printSession(os.Stdout, sess, len(history), u, contextWindow)
				continue
			case "compact":
				force = true
				compactArgs = strings.TrimSpace(arg)
			case "new":
				ns, err := session.New(projectDir)
				if err != nil {
					return fatal(err)
				}
				sess, history, ids, lastInput = ns, nil, nil, 0
				setSession(ns)
				u.Restore(banner(modelLabel), nil)
			}
			if !force {
				continue
			}
		}
		if force || (compactAt > 0 && lastInput > compactAt) {
			if h, newIDs, ok := compact(ag, sess, history, ids, u, compactArgs); ok {
				history, ids, lastInput = h, newIDs, 0
			}
		}
		if u.Interactive() {
			fmt.Fprintln(os.Stdout)
		}
		n := len(history)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		u.StartSpinner()
		newHistory, err := ag.Run(ctx, history, line, emit)
		u.StopSpinner()
		stop()
		if ctx.Err() != nil {
			u.Interrupt()
		}
		history = newHistory
		added, err := appendAll(sess, history[n:])
		if err != nil {
			return fatal(err)
		}
		ids = append(ids, added...)
	}
}

// pickerModels returns the model list shown in /model: the available catalog
// filtered to providers with usable credentials, as Pi does.
func pickerModels() []models.ModelEntry {
	return providers.AuthenticatedModels(models.AvailableModels(), auth.Path())
}

// pickerRefs returns the picker list as provider/model strings.
func pickerRefs() []string {
	models := pickerModels()
	refs := make([]string, 0, len(models))
	for _, m := range models {
		refs = append(refs, m.Ref())
	}
	return refs
}

// printModels writes the model list, marking the current model.
func printModels(w io.Writer, current string, list []models.ModelEntry) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no models in", models.ModelsPath())
		return
	}
	for _, m := range list {
		mark := "  "
		if m.Ref() == current {
			mark = "* "
		}
		fmt.Fprintf(w, "%s%s\n", mark, m.Ref())
	}
}

func appendAll(sess *session.Session, msgs []llm.Message) ([]string, error) {
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		id, err := sess.Append(m)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// compactLimit returns the input-token threshold that triggers compaction: a
// fraction of a known context window, or the fixed fallback when unknown.
func compactLimit(contextWindow int) int {
	if contextWindow > 0 {
		return int(float64(contextWindow) * config.CompactAtRatio)
	}
	return config.CompactAtTokens
}

// printSession writes the current session summary for `/session`.
func printSession(w io.Writer, sess *session.Session, messages int, u *ui.UI, contextWindow int) {
	in, out := u.UsageTotals()
	fmt.Fprintf(w, "session: %s\nid: %s\nmessages: %d\ntokens: %d in / %d out\n", sess.Path(), sess.ID(), messages, in, out)
	if contextWindow > 0 {
		fmt.Fprintf(w, "context window: %d\n", contextWindow)
	}
}

// navigateTree shows the session tree, switches the active branch to the
// chosen entry, and optionally summarizes the abandoned branch. With an
// argument (a 1-based number or an entry id prefix) it switches directly. The
// screen is cleared and the new branch replayed, as /resume does, so the old
// branch's turns do not linger on screen after the switch.
func navigateTree(ag *agent.Agent, sess *session.Session, u *ui.UI, history *[]llm.Message, ids *[]string, lastInput *int, header, arg string) {
	t, err := sess.Tree()
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return
	}
	entries := t.Entries()
	if len(entries) == 0 {
		u.Render(event.Event{Kind: event.Notice, Text: "empty session"})
		return
	}
	var id string
	switch {
	case arg != "":
		if id = matchEntry(entries, arg); id == "" {
			u.Render(event.Event{Kind: event.Error, Err: fmt.Errorf("no tree entry matches %q", arg)})
			return
		}
	case !u.Interactive():
		for i, e := range entries {
			mark := " "
			if e.Leaf {
				mark = "*"
			}
			fmt.Fprintf(os.Stdout, "%s %d) %s%s\n", mark, i+1, strings.Repeat("  ", e.Depth), e.Label)
		}
		fmt.Fprintln(os.Stdout, "switch with /tree <number>")
		return
	default:
		items := make([]ui.SelectItem, 0, len(entries))
		for _, e := range entries {
			items = append(items, ui.SelectItem{Value: e.ID, Label: strings.Repeat("  ", e.Depth) + e.Label})
		}
		id, err = u.Select("tree", items, t.Leaf)
		if err != nil {
			if !errors.Is(err, ui.ErrCancel) {
				u.Render(event.Event{Kind: event.Error, Err: err})
			}
			return
		}
	}
	if id == t.Leaf {
		return
	}
	oldLeaf := sess.Leaf()
	newHistory, newIDs, err := sess.SetLeaf(id)
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return
	}
	if abandoned := t.Abandoned(oldLeaf, id); len(abandoned) > 0 && u.Ask(fmt.Sprintf("summarize the %d messages you are leaving?", len(abandoned))) {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		u.StartSpinner()
		summary, sumErr := agent.Summarize(ctx, ag.Provider, ag.System, abandoned)
		u.StopSpinner()
		stop()
		switch {
		case sumErr != nil:
			u.Render(event.Event{Kind: event.Error, Err: sumErr})
		case summary == "":
			u.Render(event.Event{Kind: event.Notice, Text: "summary was empty; skipped"})
		default:
			if err := sess.AppendBranchSummary(oldLeaf, summary); err != nil {
				u.Render(event.Event{Kind: event.Error, Err: err})
			} else {
				newHistory, newIDs = sess.Branch()
			}
		}
	}
	*history, *ids, *lastInput = newHistory, newIDs, 0
	if u.Interactive() {
		// Clear erases the branch just left; Restore replays the chosen branch
		// so the display matches the context supplied to the next request.
		u.Restore(header, newHistory)
	}
	u.Render(event.Event{Kind: event.Notice, Text: fmt.Sprintf("switched branch: %d messages in context, ending at %s", len(newHistory), entryLabel(entries, id))})
}

// matchEntry resolves a /tree argument to an entry id: a 1-based number or a
// unique id prefix. It returns "" when nothing or more than one entry matches.
func matchEntry(entries []session.Entry, arg string) string {
	if n, err := strconv.Atoi(arg); err == nil {
		if n >= 1 && n <= len(entries) {
			return entries[n-1].ID
		}
		return ""
	}
	match := ""
	for _, e := range entries {
		if strings.HasPrefix(e.ID, arg) {
			if match != "" {
				return ""
			}
			match = e.ID
		}
	}
	return match
}

// entryLabel returns the label for an entry id, or the id when unknown.
func entryLabel(entries []session.Entry, id string) string {
	for _, e := range entries {
		if e.ID == id {
			return e.Label
		}
	}
	return id
}

// resumeSession shows the session picker and switches to the chosen session.
// It reports whether the active session changed.
func resumeSession(u *ui.UI, sess **session.Session, history *[]llm.Message, ids *[]string, lastInput *int, header, projectDir string, setSession func(*session.Session)) bool {
	infos, err := session.List(projectDir)
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return false
	}
	if len(infos) == 0 {
		u.Render(event.Event{Kind: event.Notice, Text: "no sessions"})
		return false
	}
	if !u.Interactive() {
		for _, in := range infos {
			fmt.Fprintln(os.Stdout, in.Label())
		}
		return false
	}
	items := make([]ui.SelectItem, 0, len(infos))
	for _, in := range infos {
		items = append(items, ui.SelectItem{Value: in.Path, Label: in.Label()})
	}
	path, err := u.Select("resume", items, (*sess).Path())
	if err != nil {
		return false
	}
	ns, h, newIDs, err := session.Open(path)
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return false
	}
	_ = (*sess).Close()
	*sess, *history, *ids, *lastInput = ns, h, newIDs, 0
	setSession(ns)
	u.Restore(header, h)
	u.Render(event.Event{Kind: event.Notice, Text: "resumed " + ns.ID()})
	return true
}

// compact summarizes older messages. It reports whether history changed and
// returns the history and record ids with the summary prepended.
func compact(ag *agent.Agent, sess *session.Session, history []llm.Message, ids []string, u *ui.UI, instructions string) ([]llm.Message, []string, bool) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	u.StartSpinner()
	newHistory, summary, cut, err := agent.Compact(ctx, ag.Provider, ag.System, history, config.KeepRecentMsgs, instructions)
	u.StopSpinner()
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return history, ids, false
	}
	if cut == 0 {
		u.Render(event.Event{Kind: event.Notice, Text: "nothing to compact"})
		return history, ids, false
	}
	firstKept := ""
	if cut < len(ids) {
		firstKept = ids[cut]
	}
	if err := sess.AppendCompaction(summary, firstKept); err != nil {
		fmt.Fprintln(os.Stderr, "pigo: session:", err)
	}
	newIDs := append([]string{""}, ids[cut:]...)
	u.Render(event.Event{Kind: event.Notice, Text: "compacted context"})
	return newHistory, newIDs, true
}
