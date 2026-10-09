// Package session stores conversations as append-only JSONL files.
package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pigo/internal/llm"
)

type record struct {
	Time        time.Time    `json:"t"`
	Type        string       `json:"type"`
	ID          string       `json:"id,omitempty"`
	ParentID    string       `json:"parent_id,omitempty"`
	Message     *llm.Message `json:"message,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	FirstKeptID string       `json:"first_kept_id,omitempty"`
	Replaces    int          `json:"replaces,omitempty"`
	LeafID      string       `json:"leaf_id,omitempty"`
	FromID      string       `json:"from_id,omitempty"`
}

// Node is one entry in the session tree.
type Node struct {
	ID          string
	ParentID    string
	Type        string // "message", "compaction", or "branch_summary"
	Message     *llm.Message
	Summary     string
	FirstKeptID string
	Replaces    int
	Children    []string
}

// Tree is a parsed session: entries linked by parent id plus the active leaf.
type Tree struct {
	Nodes map[string]*Node
	Order []string // entry ids in file order
	Leaf  string   // active leaf id
}

// Entry is a flattened tree row for the /tree picker.
type Entry struct {
	ID     string
	Depth  int
	Label  string
	Active bool // on the branch ending at the active leaf
	Leaf   bool
}

// Session is an open append-only session file.
type Session struct {
	path string
	f    *os.File
	leaf string
}

// StateDir returns the directory for pigo state, honoring XDG_STATE_HOME.
func StateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "pigo")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".pigo"
	}
	return filepath.Join(home, ".local", "state", "pigo")
}

// ModelRefPath returns the file that remembers the last model chosen with
// /model or Ctrl+L.
func ModelRefPath() string { return filepath.Join(StateDir(), "model") }

// LoadModelRef reads the remembered model reference, or "" when none is set.
func LoadModelRef() string {
	b, err := os.ReadFile(ModelRefPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SaveModelRef remembers ref as the last selected model. Failures are ignored:
// the choice is a convenience, not session state.
func SaveModelRef(ref string) {
	if ref == "" || ref == "none" {
		return
	}
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(ModelRefPath(), []byte(ref+"\n"), 0o600)
}

// New creates a fresh session file in dir.
func New(dir string) (*Session, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := time.Now().UTC().Format("20060102T150405") + "-" + shortID() + ".jsonl"
	return open(filepath.Join(dir, name))
}

// Open opens an existing session file and replays its history and record ids.
func Open(path string) (*Session, []llm.Message, []string, error) {
	s, err := open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	t, err := parse(path)
	if err != nil {
		_ = s.f.Close()
		return nil, nil, nil, err
	}
	s.leaf = t.validLeaf(t.Leaf)
	history, ids := t.branch(s.leaf)
	return s, history, ids, nil
}

func open(path string) (*Session, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Session{path: path, f: f}, nil
}

// Path returns the session file path.
func (s *Session) Path() string { return s.path }

// ID returns the session file's base name without the .jsonl extension.
func (s *Session) ID() string {
	return strings.TrimSuffix(filepath.Base(s.path), ".jsonl")
}

// Leaf returns the active leaf entry id.
func (s *Session) Leaf() string { return s.leaf }

// Tree parses the session file into an entry tree with the active leaf.
func (s *Session) Tree() (*Tree, error) { return parse(s.path) }

// Branch returns the active branch's history and record ids.
func (s *Session) Branch() ([]llm.Message, []string) {
	t, err := parse(s.path)
	if err != nil {
		return nil, nil
	}
	return t.branch(t.Leaf)
}

// SetLeaf points the active branch at the entry id and returns that branch's
// history and record ids. The choice is persisted so a later resume keeps it.
func (s *Session) SetLeaf(id string) ([]llm.Message, []string, error) {
	t, err := parse(s.path)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := t.Nodes[id]; !ok {
		return nil, nil, fmt.Errorf("no session entry %q", id)
	}
	// An assistant message whose tool calls have no results cannot end a
	// branch; continue from its parent instead so the next request is valid.
	id = t.validLeaf(id)
	if err := s.write(record{Time: time.Now().UTC(), Type: "leaf", LeafID: id}); err != nil {
		return nil, nil, err
	}
	s.leaf = id
	history, ids := t.branch(id)
	return history, ids, nil
}

// Append writes one message record and returns its id.
func (s *Session) Append(m llm.Message) (string, error) {
	id := shortID()
	rec := record{Time: time.Now().UTC(), Type: "message", ID: id, ParentID: s.leaf, Message: &m}
	if err := s.write(rec); err != nil {
		return "", err
	}
	s.leaf = id
	return id, nil
}

// AppendCompaction records a summary that replaces history before the message
// with firstKeptID.
func (s *Session) AppendCompaction(summary, firstKeptID string) error {
	id := shortID()
	rec := record{Time: time.Now().UTC(), Type: "compaction", ID: id, ParentID: s.leaf, Summary: summary, FirstKeptID: firstKeptID}
	if err := s.write(rec); err != nil {
		return err
	}
	s.leaf = id
	return nil
}

// AppendBranchSummary records a summary of the branch abandoned when moving
// from fromID and makes it the new active leaf.
func (s *Session) AppendBranchSummary(fromID, summary string) error {
	id := shortID()
	rec := record{Time: time.Now().UTC(), Type: "branch_summary", ID: id, ParentID: s.leaf, FromID: fromID, Summary: summary}
	if err := s.write(rec); err != nil {
		return err
	}
	s.leaf = id
	return nil
}

func (s *Session) write(r record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = s.f.Write(b)
	return err
}

// Close closes the session file.
func (s *Session) Close() error { return s.f.Close() }

// OpenOrNew opens the session named by resume, the latest when cont is true,
// or a new session in dir.
func OpenOrNew(resume string, cont bool, dir string) (*Session, []llm.Message, []string, error) {
	if resume != "" {
		return Open(resume)
	}
	if cont {
		path, err := Latest(dir)
		if err != nil {
			return nil, nil, nil, err
		}
		return Open(path)
	}
	s, err := New(dir)
	return s, nil, nil, err
}

// Latest returns the most recently modified session file in dir.
func Latest(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var newest string
	var newestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = filepath.Join(dir, e.Name())
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no sessions in %s", dir)
	}
	return newest, nil
}

// parse reads the session file into an entry tree. Entries without an explicit
// parent are chained to the previous entry so legacy files stay linear.
func parse(path string) (*Tree, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	t := &Tree{Nodes: map[string]*Node{}}
	msgSeq := 0
	last := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			fmt.Fprintf(os.Stderr, "pigo: skipping corrupt session line: %v\n", err)
			continue
		}
		switch r.Type {
		case "message":
			if r.Message == nil {
				continue
			}
			id := r.ID
			if id == "" {
				// Legacy records have no id; derive a stable one from position.
				id = fmt.Sprintf("r%d", msgSeq)
			}
			msgSeq++
			last = t.add(&Node{ID: id, ParentID: parentOf(r.ParentID, last), Type: "message", Message: r.Message})
		case "compaction":
			id := r.ID
			if id == "" {
				id = fmt.Sprintf("c%d", len(t.Order))
			}
			last = t.add(&Node{ID: id, ParentID: parentOf(r.ParentID, last), Type: "compaction", Summary: r.Summary, FirstKeptID: r.FirstKeptID, Replaces: r.Replaces})
		case "branch_summary":
			id := r.ID
			if id == "" {
				id = fmt.Sprintf("b%d", len(t.Order))
			}
			last = t.add(&Node{ID: id, ParentID: parentOf(r.ParentID, last), Type: "branch_summary", Summary: r.Summary})
		case "leaf":
			if _, ok := t.Nodes[r.LeafID]; ok {
				t.Leaf = r.LeafID
			}
		}
	}
	if _, ok := t.Nodes[t.Leaf]; !ok {
		t.Leaf = last
	}
	return t, sc.Err()
}

func parentOf(parent, last string) string {
	if parent != "" {
		return parent
	}
	return last
}

// add registers a node, links it to its parent, and makes it the active leaf.
func (t *Tree) add(n *Node) string {
	t.Nodes[n.ID] = n
	t.Order = append(t.Order, n.ID)
	if p, ok := t.Nodes[n.ParentID]; ok {
		p.Children = append(p.Children, n.ID)
	}
	t.Leaf = n.ID
	return n.ID
}

// branch walks from leaf to the root and applies compaction records, returning
// the model-visible history and the record id for each history entry ("" for a
// compaction or branch summary message).
func (t *Tree) branch(leaf string) ([]llm.Message, []string) {
	path := t.ancestors(leaf)
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	var history []llm.Message
	var ids []string
	for _, id := range path {
		n := t.Nodes[id]
		if n == nil {
			continue
		}
		switch n.Type {
		case "message":
			if n.Message == nil {
				continue
			}
			history = append(history, *n.Message)
			ids = append(ids, n.ID)
		case "compaction":
			cut := -1
			switch {
			case n.FirstKeptID != "":
				cut = indexOf(ids, n.FirstKeptID)
				if cut < 0 {
					fmt.Fprintf(os.Stderr, "pigo: compaction boundary %q not found; keeping history\n", n.FirstKeptID)
					continue
				}
			case n.Replaces > 0:
				if n.Replaces < len(history) {
					cut = n.Replaces
				} else {
					history, ids, cut = nil, nil, 0
				}
			default:
				continue
			}
			if cut > 0 {
				history, ids = history[cut:], ids[cut:]
			}
			history = append([]llm.Message{{Role: llm.RoleUser, Text: "[Earlier conversation summary]\n" + n.Summary}}, history...)
			ids = append([]string{""}, ids...)
		case "branch_summary":
			history = append(history, llm.Message{Role: llm.RoleUser, Text: "[Branch summary]\n" + n.Summary})
			ids = append(ids, "")
		}
	}
	return history, ids
}

// validLeaf returns leaf, or its parent when leaf would end the branch with an
// assistant message whose tool calls have no results yet.
func (t *Tree) validLeaf(leaf string) string {
	n := t.Nodes[leaf]
	if n != nil && n.Type == "message" && n.Message != nil &&
		n.Message.Role == llm.RoleAssistant && len(n.Message.Calls) > 0 {
		return n.ParentID
	}
	return leaf
}

// ancestors returns the node ids from leaf up to the root.
func (t *Tree) ancestors(leaf string) []string {
	var out []string
	seen := map[string]bool{}
	for id := leaf; id != ""; {
		n := t.Nodes[id]
		if n == nil || seen[id] {
			break
		}
		seen[id] = true
		out = append(out, id)
		id = n.ParentID
	}
	return out
}

// Abandoned returns the messages on the path from fromLeaf up to, but not
// including, its lowest common ancestor with toLeaf, in conversation order.
func (t *Tree) Abandoned(fromLeaf, toLeaf string) []llm.Message {
	onNew := map[string]bool{}
	for _, id := range t.ancestors(toLeaf) {
		onNew[id] = true
	}
	var ids []string
	for _, id := range t.ancestors(fromLeaf) {
		if onNew[id] {
			break
		}
		ids = append(ids, id)
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	var out []llm.Message
	for _, id := range ids {
		if n := t.Nodes[id]; n != nil && n.Type == "message" && n.Message != nil {
			out = append(out, *n.Message)
		}
	}
	return out
}

// Entries flattens the tree depth-first for the /tree picker, marking nodes on
// the active branch and the active leaf.
func (t *Tree) Entries() []Entry {
	active := map[string]bool{}
	for _, id := range t.ancestors(t.Leaf) {
		active[id] = true
	}
	var out []Entry
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		n := t.Nodes[id]
		if n == nil {
			return
		}
		out = append(out, Entry{ID: n.ID, Depth: depth, Label: n.label(), Active: active[n.ID], Leaf: n.ID == t.Leaf})
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for _, id := range t.Order {
		if n := t.Nodes[id]; n != nil && t.Nodes[n.ParentID] == nil {
			walk(id, 0)
		}
	}
	return out
}

// label is the one-line description shown for a node.
func (n *Node) label() string {
	switch n.Type {
	case "message":
		if n.Message == nil {
			return "message"
		}
		text := preview(n.Message.Text)
		if text == "" && len(n.Message.Calls) > 0 {
			names := make([]string, 0, len(n.Message.Calls))
			for _, c := range n.Message.Calls {
				names = append(names, c.Name)
			}
			text = strings.Join(names, ", ")
		}
		if text == "" && len(n.Message.Results) > 0 {
			text = fmt.Sprintf("%d tool result(s)", len(n.Message.Results))
		}
		return string(n.Message.Role) + ": " + text
	case "compaction":
		return "compaction: " + preview(n.Summary)
	case "branch_summary":
		return "branch summary: " + preview(n.Summary)
	}
	return n.Type
}

func preview(s string) string {
	text := strings.Join(strings.Fields(s), " ")
	if r := []rune(text); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return text
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

// Info describes a session file for the resume picker.
type Info struct {
	Path      string
	ID        string
	ModTime   time.Time
	Messages  int
	FirstUser string
}

// Label is the one-line description shown in the picker.
func (i Info) Label() string {
	text := strings.Join(strings.Fields(i.FirstUser), " ")
	if r := []rune(text); len(r) > 50 {
		text = string(r[:50]) + "…"
	}
	return fmt.Sprintf("%s  %s  (%d msgs)  %s", i.ModTime.Local().Format("2006-01-02 15:04"), i.ID, i.Messages, text)
}

// List returns session files in dir, newest first.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t, err := parse(path)
		if err != nil {
			continue
		}
		history, _ := t.branch(t.Leaf)
		first := ""
		for _, m := range history {
			if m.Role == llm.RoleUser && !strings.HasPrefix(m.Text, "[Earlier conversation summary]") {
				first = m.Text
				break
			}
		}
		out = append(out, Info{Path: path, ID: strings.TrimSuffix(e.Name(), ".jsonl"), ModTime: fi.ModTime(), Messages: len(history), FirstUser: first})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

func shortID() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	}
	return hex.EncodeToString(b[:])
}
