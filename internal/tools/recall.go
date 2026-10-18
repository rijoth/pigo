package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

const recallDescription = `Recall earlier parts of this session, including anything dropped by compaction. Use it before saying the context is gone.

- query: keyword search (case-insensitive; a valid regex per word also works). Multi-word queries return entries containing every word.
- ref: the #N number from a summary ref like (#12); returns that entry in full.
- mode: "touched": list files worked on with their #N refs.
- scope: "lineage" (default) reads the active conversation path; "all" also reads abandoned branches.
- page and limit: page through query or recent results (defaults: page 1, limit 5).

#N is the number shown in summary refs such as (#12) and in recall results. Only the current session is searchable.`

var recallSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string",  "description": "Keyword search. Every word must appear (case-insensitive; each word may be a regex). Omit to list the most recent entries."},
    "ref":   {"type": "integer", "description": "#N from a summary ref or recall result; returns that entry in full."},
    "mode":  {"type": "string",  "enum": ["hybrid", "touched"], "description": "\"touched\" lists files worked on with their #N refs."},
    "scope": {"type": "string",  "enum": ["lineage", "all"], "description": "\"lineage\" (default) is the active branch; \"all\" includes abandoned branches."},
    "limit": {"type": "integer", "description": "Results per page (default 5)."},
    "page":  {"type": "integer", "description": "1-based page (default 1)."}
  }
}`)

// Recall is a session-backed tool: the search itself is supplied by the
// session store, keeping this package free of a session dependency.
type Recall struct {
	Fn func(context.Context, json.RawMessage) (string, error)
}

// Spec returns the model-visible tool description.
func (Recall) Spec() Spec {
	return Spec{Name: "pigo_recall", Description: recallDescription, Schema: recallSchema}
}

// Run implements Tool.
func (r Recall) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if r.Fn == nil {
		return "", fmt.Errorf("recall is unavailable")
	}
	return r.Fn(ctx, args)
}
