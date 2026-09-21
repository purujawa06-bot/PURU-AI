package ai

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Exec timeout policy: default 60s when the agent omits timeout_seconds,
// hard cap 300s. On timeout the whole process group is killed so no RAM
// is left behind.
const (
	defaultExecTimeoutSec = 60
	maxExecTimeoutSec     = 300
)

type execResult struct {
	Success  bool   `json:"success"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// clampTimeout normalizes agent input to [1, maxExecTimeoutSec], default 60.
func clampTimeout(v any) int {
	secs := defaultExecTimeoutSec
	switch n := v.(type) {
	case int:
		secs = n
	case int64:
		secs = int(n)
	case float64:
		secs = int(n)
	}
	if secs <= 0 {
		secs = defaultExecTimeoutSec
	}
	if secs > maxExecTimeoutSec {
		secs = maxExecTimeoutSec
	}
	return secs
}

// runExec runs command via shell in dir with timeoutSec. The process is
// started in its own process group (unix) so timeout kills the PID group,
// not just the shell — children can't leak RAM.
func runExec(dir, command string, timeoutSec int) execResult {
	if strings.TrimSpace(command) == "" {
		return execResult{Success: false, ExitCode: -1, Output: "command kosong"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	shell, prefix := defaultShell()
	args := append(prefix, command)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = dir
	setupKillGroup(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Start()
	if runErr != nil {
		return execResult{Success: false, ExitCode: -1, Output: runErr.Error()}
	}
	pid := -1
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	waitErr := cmd.Wait()

	out := buf.String()
	if len(out) > maxExecOutput {
		out = out[:maxExecOutput] + "\n...[truncated]"
	}

	if ctx.Err() == context.DeadlineExceeded {
		killGroup(pid)
		return execResult{Success: false, ExitCode: -1, Output: out + fmt.Sprintf("\n[timeout %ds — proses di-kill]", timeoutSec), TimedOut: true}
	}
	if waitErr != nil {
		code := -1
		if ee, ok := waitErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return execResult{Success: false, ExitCode: code, Output: out}
	}
	return execResult{Success: true, ExitCode: 0, Output: out}
}
