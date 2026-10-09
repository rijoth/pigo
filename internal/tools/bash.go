package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"pigo/internal/config"
)

const bashDescription = `Run a command with bash -c in the working directory. Returns combined stdout and stderr plus the exit code. Set timeout in seconds to override the default.`

var bashSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string",  "description": "Shell command to run."},
    "timeout": {"type": "integer", "description": "Timeout in seconds."}
  },
  "required": ["command"]
}`)

// Bash runs a shell command.
type Bash struct{}

// Spec returns the model-visible tool description.
func (Bash) Spec() Spec {
	return Spec{Name: "bash", Description: bashDescription, Schema: bashSchema}
}

type bashArgs struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// Run implements Tool.
func (Bash) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var a bashArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if a.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	out, code, err := RunShell(ctx, a.Command, a.Timeout)
	result := fmt.Sprintf("exit code: %d\n%s", code, out)
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, result)
	}
	if code != 0 {
		return "", fmt.Errorf("exit status %d\n%s", code, result)
	}
	return result, nil
}

// RunShell runs command with bash -c using the same timeout, process-group
// kill, and tail truncation as the bash tool. It returns the combined output
// and exit code. err is non-nil only when the command cannot start or times out.
func RunShell(ctx context.Context, command string, timeoutSecs int) (out string, code int, err error) {
	if timeoutSecs <= 0 {
		timeoutSecs = config.BashTimeoutSecs
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.Command("bash", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return "", -1, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-tctx.Done():
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-done
		err = fmt.Errorf("command timed out after %ds", timeoutSecs)
	case <-done:
	}
	out = truncateTail(buf.String(), config.MaxBashBytes)
	code = -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return out, code, err
}
