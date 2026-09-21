package ai

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// Exec timeout policy: default 60s when the agent omits timeout_seconds,
// hard cap 300s. On timeout the whole process group is killed so no RAM
// is left behind.
const (
	defaultExecTimeoutSec = 60
	maxExecTimeoutSec     = 300
)

// Exec RAM policy: each command's whole process group is capped at memMB
// resident memory (default 256MB, min 64MB via config exec_memory_mb).
// Over budget → SIGKILL the group. Files written by commands are capped at
// maxExecFileMB via ulimit -f (unix only, best-effort).
const (
	defaultExecMemMB = 64
	minExecMemMB     = 64
	maxExecFileMB    = 100
)

// maxExecFileBlocks is maxExecFileMB in ulimit -f units (512-byte blocks).
const maxExecFileBlocks = maxExecFileMB * 1024 * 1024 / 512

type execResult struct {
	Success       bool   `json:"success"`
	ExitCode      int    `json:"exit_code"`
	Output        string `json:"output"`
	TimedOut      bool   `json:"timed_out,omitempty"`
	MemoryLimited bool   `json:"memory_limited,omitempty"`
}

// clampMemMB normalizes the exec RAM budget: <=0 → default, below min → min.
func clampMemMB(v int) int {
	if v <= 0 {
		return defaultExecMemMB
	}
	if v < minExecMemMB {
		return minExecMemMB
	}
	return v
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

// cappedWriter collects at most max output bytes; the rest is discarded and
// flagged. Truncation happens DURING capture, so giant outputs (cat file
// besar) never sit fully in RAM before being cut — the buffer itself is
// the cap, and the "...[truncated]" marker tells the agent output was cut.
type cappedWriter struct {
	buf   []byte
	max   int
	total int
	cut   bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.total += n
	if w.total > w.max {
		w.cut = true
	}
	if remaining := w.max - len(w.buf); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		w.buf = append(w.buf, p...)
	}
	return n, nil // discarded bytes still count as consumed
}

func (w *cappedWriter) String() string {
	if w.cut {
		return string(w.buf) + "\n...[truncated]"
	}
	return string(w.buf)
}

// runExec runs command via shell in dir with timeoutSec and a memMB resident-
// memory budget for the whole process group. The process starts in its own
// process group (unix) so timeout/memory kills hit the PID group, not just
// the shell — children can't leak RAM.
func runExec(dir, command string, timeoutSec, memMB int) execResult {
	if strings.TrimSpace(command) == "" {
		return execResult{Success: false, ExitCode: -1, Output: "command kosong"}
	}
	memMB = clampMemMB(memMB)
	if runtime.GOOS != "windows" {
		// Cap file sizes written by the command (100MB); best-effort.
		command = fmt.Sprintf("ulimit -f %d 2>/dev/null; %s", maxExecFileBlocks, command)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	shell, prefix := defaultShell()
	args := append(prefix, command)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = dir
	setupKillGroup(cmd)
	outW := &cappedWriter{max: maxExecOutput}
	cmd.Stdout = outW
	cmd.Stderr = outW

	runErr := cmd.Start()
	if runErr != nil {
		return execResult{Success: false, ExitCode: -1, Output: runErr.Error()}
	}
	pid := -1
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	// Watch group RSS on Linux; no-op elsewhere. stop closed after Wait.
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	var memKilled atomic.Bool
	startMemWatch(pid, int64(memMB)<<20, stopWatch, &memKilled)
	waitErr := cmd.Wait()

	out := outW.String()

	if memKilled.Load() {
		return execResult{Success: false, ExitCode: -1,
			Output:        out + fmt.Sprintf("\n[memory limit %dMB — grup proses di-kill]", memMB),
			MemoryLimited: true}
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
