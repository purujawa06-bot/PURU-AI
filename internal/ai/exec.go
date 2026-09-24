package ai

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	sessions   = make(map[string]*execSession)
	sessionsMu sync.RWMutex
)

type execSession struct {
	mu            sync.RWMutex
	ID            string
	Cmd           *exec.Cmd
	Output        *cappedWriter
	Cancel        context.CancelFunc
	StartTime     time.Time
	EndTime       time.Time
	IsRunning     bool
	TimedOut      bool
	MemoryLimited bool
	// killOnce guarantees the TERM-first escalation is scheduled exactly
	// once even when timeout, parent-cancel and kill race each other.
	killOnce sync.Once
}

// Exec timeout policy: default 60s when the agent omits timeout,
// hard cap 300s. On timeout the whole process group is killed so no RAM
// is left behind.
const (
	defaultExecTimeoutSec = 60
	maxExecTimeoutSec     = 300
)

// Exec RAM policy: each command's whole process group is capped at memMB
// resident memory (default = min 64MB via config exec_memory_mb).
// Over budget → SIGKILL the group. Files written by commands are capped at
// maxExecFileMB via ulimit -f (unix only, best-effort).
const (
	defaultExecMemMB = 64
	minExecMemMB     = 64
	maxExecFileMB    = 100
)

// maxExecFileBlocks is maxExecFileMB in ulimit -f units (512-byte blocks).
const maxExecFileBlocks = maxExecFileMB * 1024 * 1024 / 512

// maxExecSessions caps stored exec sessions so blocking/background runs
// never grow the map (and its 20k output buffers) without bound.
const maxExecSessions = 20

// terminateGrace is the delay between SIGTERM and the SIGKILL escalation.
// SIGTERM first lets the shell reap its children (git, tar, ssl_client);
// an immediate SIGKILL kills the shell too and orphans them under PID 1
// as zombies. The delayed SIGKILL still guarantees termination when a
// command traps or ignores SIGTERM.
const terminateGrace = 500 * time.Millisecond

// terminateGroup asks the process group to exit gracefully, then escalates
// to SIGKILL after terminateGrace. It never blocks: the timer fires even if
// the caller already moved on. Killing a dead group is a harmless no-op.
// Orphaned grandchildren that still end up under PID 1 are collected by the
// SIGCHLD reaper installed via StartReaper.
func terminateGroup(pid int) {
	if pid <= 0 {
		return
	}
	killGroupTerm(pid)
	time.AfterFunc(terminateGrace, func() {
		killGroup(pid)
	})
}

// requestTerminate schedules the TERM-first escalation exactly once per
// session so concurrent timeout/cancel/kill paths never stack timers.
func (s *execSession) requestTerminate(pid int) {
	if s == nil || pid <= 0 {
		return
	}
	s.killOnce.Do(func() {
		terminateGroup(pid)
	})
}

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
// the cap, and the "...[truncated, total X]" marker tells the agent output was cut.
// Thread-safe: Write runs on the process goroutine while String can be read
// concurrently via exec read/poll.
type cappedWriter struct {
	mu    sync.Mutex
	buf   []byte
	max   int
	total int
	cut   bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
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
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cut {
		return string(w.buf) + fmt.Sprintf("\n...[truncated, total %s]", formatSize(int64(w.total)))
	}
	return string(w.buf)
}

// snapshot copies session status under lock so poll/read/kill/list never race
// the watcher goroutine that marks completion.
func (s *execSession) snapshot() (running bool, timedOut bool, memLimited bool, start time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.IsRunning, s.TimedOut, s.MemoryLimited, s.StartTime
}

func (s *execSession) finish(timedOut bool, memLimited bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.IsRunning = false
	s.EndTime = time.Now()
	s.TimedOut = timedOut
	s.MemoryLimited = memLimited
}

// runExec runs command via shell in dir with timeoutSec and a memMB resident-
// memory budget for the whole process group. The process starts in its own
// process group (unix) so timeout/memory kills hit the PID group, not just
// the shell — children can't leak RAM.
func runExec(parent context.Context, dir, command string, timeoutSec, memMB int, background bool) (any, error) {
	if strings.TrimSpace(command) == "" {
		return execResult{Success: false, ExitCode: -1, Output: "command is empty"}, nil
	}
	if parent == nil {
		parent = context.Background()
	}
	if background {
		// Sesi background hidup lintas request (dipoll/diread/dikill belakangan),
		// jadi timeout-nya dilepas dari cancel request: /stop hanya menghentikan
		// run agent yang sedang berjalan, bukan sesi background (pakai kill).
		parent = context.WithoutCancel(parent)
	}
	memMB = clampMemMB(memMB)
	if runtime.GOOS != "windows" {
		command = fmt.Sprintf("ulimit -f %d 2>/dev/null; %s", maxExecFileBlocks, command)
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
	shell, prefix := defaultShell()
	args := append(prefix, command)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = dir
	setupKillGroup(cmd)

	outW := &cappedWriter{max: maxExecOutput}
	cmd.Stdout = outW
	cmd.Stderr = outW

	err := cmd.Start()
	if err != nil {
		cancel()
		return execResult{Success: false, ExitCode: -1, Output: err.Error()}, nil
	}

	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	sess := &execSession{
		ID:        sessionID,
		Cmd:       cmd,
		Output:    outW,
		Cancel:    cancel,
		StartTime: time.Now(),
		IsRunning: true,
	}

	sessionsMu.Lock()
	sessions[sessionID] = sess
	pruneSessionsLocked()
	sessionsMu.Unlock()

	pid := -1
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	go func() {
		stopWatch := make(chan struct{})
		var memKilled atomic.Bool
		startMemWatch(pid, int64(memMB)<<20, stopWatch, &memKilled)

		_ = cmd.Wait()
		close(stopWatch)
		cancel()

		timedOut := ctx.Err() == context.DeadlineExceeded
		sess.finish(timedOut, memKilled.Load())
		if timedOut {
			// Background runs have no blocking loop watching ctx, so the
			// group outlives the shell here without this escalation.
			// TERM-first: the shell (if still alive) reaps its children,
			// leftovers are SIGKILLed by the timer, orphans by the reaper.
			sess.requestTerminate(pid)
		}
	}()

	if background {
		return map[string]any{"success": true, "sessionId": sessionID, "status": "running"}, nil
	}

	// Wait for completion if not background. /stop (parent ctx cancel) ikut
	// membunuh grup proses agar run blocking langsung berhenti, bukan nunggu
	// timeout sendiri (maks 300 dtk).
	for {
		running, _, _, start := sess.snapshot()
		if !running {
			break
		}
		select {
		case <-ctx.Done():
			if sess.Cmd != nil && sess.Cmd.Process != nil {
				sess.requestTerminate(sess.Cmd.Process.Pid)
			}
		case <-time.After(100 * time.Millisecond):
		}
		if time.Since(start) > time.Duration(timeoutSec+2)*time.Second {
			break // safety break
		}
	}

	running, timedOut, memLimited, _ := sess.snapshot()
	res := execResult{
		Success:       sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Success() && !running,
		ExitCode:      -1,
		Output:        sess.Output.String(),
		TimedOut:      timedOut,
		MemoryLimited: memLimited,
	}
	if sess.Cmd.ProcessState != nil {
		res.ExitCode = sess.Cmd.ProcessState.ExitCode()
	}
	return res, nil
}

func lookupSession(sessionID string) (*execSession, bool) {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	sess, ok := sessions[sessionID]
	return sess, ok
}

// pruneSessionsLocked evicts oldest finished sessions beyond maxExecSessions.
// Caller must hold sessionsMu (write). Running sessions are never evicted so
// poll/read/kill on live processes keep working; the leak fixed here is the
// unbounded pile-up of finished (blocking + background) sessions, each
// holding a 20k output buffer.
func pruneSessionsLocked() {
	if len(sessions) <= maxExecSessions {
		return
	}
	type entry struct {
		id    string
		start time.Time
	}
	var finished []entry
	for id, sess := range sessions {
		running, _, _, start := sess.snapshot()
		if !running {
			finished = append(finished, entry{id, start})
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].start.Before(finished[j].start) })
	for _, e := range finished {
		if len(sessions) <= maxExecSessions {
			break
		}
		delete(sessions, e.id)
	}
}

func pollExec(sessionID string) (any, error) {
	sess, ok := lookupSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	running, timedOut, memLimited, _ := sess.snapshot()
	status := "running"
	if !running {
		status = "finished"
	}

	return map[string]any{
		"sessionId":      sess.ID,
		"status":         status,
		"running":        running,
		"timed_out":      timedOut,
		"memory_limited": memLimited,
	}, nil
}

func readExec(sessionID string) (any, error) {
	sess, ok := lookupSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	running, timedOut, memLimited, _ := sess.snapshot()
	status := "running"
	if !running {
		status = "finished"
	}
	return map[string]any{
		"sessionId":      sess.ID,
		"status":         status,
		"output":         sess.Output.String(),
		"running":        running,
		"timed_out":      timedOut,
		"memory_limited": memLimited,
	}, nil
}

func killExec(sessionID string) (any, error) {
	sess, ok := lookupSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	running, timedOut, memLimited, _ := sess.snapshot()
	if running {
		sess.Cancel()
		if sess.Cmd != nil && sess.Cmd.Process != nil {
			sess.requestTerminate(sess.Cmd.Process.Pid)
		}
		// Tandai selesai langsung agar poll/read/list tak lagi lihat
		// running=true selama jeda sampai watcher cmd.Wait() jalan.
		sess.finish(timedOut, memLimited)
		return map[string]any{"sessionId": sessionID, "status": "killed"}, nil
	}
	return map[string]any{"sessionId": sessionID, "status": "already finished"}, nil
}

func listSessions() any {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	var list []map[string]any
	for id, sess := range sessions {
		running, _, _, start := sess.snapshot()
		list = append(list, map[string]any{
			"sessionId": id,
			"running":   running,
			"start":     start.Format(time.RFC3339),
		})
	}
	if len(list) == 0 {
		return "No active sessions."
	}
	sort.Slice(list, func(i, j int) bool {
		si, _ := list[i]["sessionId"].(string)
		sj, _ := list[j]["sessionId"].(string)
		return si < sj
	})
	return list
}
