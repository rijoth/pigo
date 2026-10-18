package compact

import "strings"

// pathKeys are the argument names that may carry a file path.
var pathKeys = []string{"path", "file_path", "filePath", "file"}

// extractPath returns the first path-like argument, or "".
func extractPath(args map[string]any) string {
	if args == nil {
		return ""
	}
	for _, key := range pathKeys {
		if v, ok := args[key].(string); ok {
			return v
		}
	}
	return ""
}

// summarizeToolArgs renders a tool call's identifying argument.
func summarizeToolArgs(args map[string]any) string {
	if p := extractPath(args); p != "" {
		return "path=" + p
	}
	if cmd, ok := args["command"].(string); ok {
		return "command=" + cmd
	}
	if q, ok := args["query"].(string); ok {
		return "query=" + q
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}
