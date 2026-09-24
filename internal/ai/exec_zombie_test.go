//go:build linux

package ai

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// statPgrpState extracts (pgrp, state) from /proc/<pid>/stat content.
// The state field is the first token after the comm parentheses.
func statPgrpState(b []byte) (int, string, bool) {
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, "", false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 3 {
		return 0, "", false
	}
	g, err := strconv.Atoi(f[2])
	if err != nil {
		return 0, "", false
	}
	return g, f[0], true
}

// liveGroupMembers counts non-zombie processes in pgid. Zombies are excluded:
// under CI they belong to init which reaps them, under production to our
// SIGCHLD reaper. A live leak (sleep/git/tar still running) fails the test.
func liveGroupMembers(pgid int) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		g, state, ok := statPgrpState(b)
		if ok && g == pgid && state != "Z" {
			n++
		}
	}
	return n
}

// waitGroupQuiet polls until no live member of pgid remains (or timeout).
func waitGroupQuiet(t *testing.T, pgid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if liveGroupMembers(pgid) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("process group %d still has %d live members after %v", pgid, liveGroupMembers(pgid), timeout)
}

func requireSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	StartReaper()
}

// terminateGroup must kill the whole tree (shell + background children),
// not just the group leader. Before the fix this used a bare SIGKILL that
// orphaned grandchildren under PID 1 as zombies.
func TestTerminateGroupKillsTree(t *testing.T) {
	requireSh(t)
	cmd := exec.Command("sh", "-c", "sleep 30 & sleep 30 & wait")
	setupKillGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pgid := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	terminateGroup(pgid)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("shell did not exit after terminateGroup")
	}
	waitGroupQuiet(t, pgid, 10*time.Second)
}

// Killing a background tree session must finish the session and leave no
// live processes behind in its group.
func TestKillExecTreeLeavesNoLiveMembers(t *testing.T) {
	requireSh(t)
	out, _ := runExec(context.Background(), t.TempDir(), "sleep 30 & sleep 30 & wait", 60, 64, true)
	m, _ := out.(map[string]any)
	sid, _ := m["sessionId"].(string)
	if sid == "" {
		t.Fatalf("background run did not return a session: %v", out)
	}
	sess, ok := lookupSession(sid)
	if !ok {
		t.Fatalf("session %s not found", sid)
	}
	pgid := sess.Cmd.Process.Pid

	if _, err := killExec(sid); err != nil {
		t.Fatalf("killExec: %v", err)
	}
	if running, _, _, _ := sess.snapshot(); running {
		t.Fatalf("session must be finished right after kill")
	}
	waitGroupQuiet(t, pgid, 10*time.Second)
}

// A blocking tree run that hits its timeout must report timed_out and
// return promptly instead of hanging until the full timeout+margin.
func TestRunExecTimeoutTreeReportsTimedOut(t *testing.T) {
	requireSh(t)
	start := time.Now()
	resAny, _ := runExec(context.Background(), t.TempDir(), "sleep 30 & sleep 30 & wait", 2, 64, false)
	res, _ := resAny.(execResult)
	if !res.TimedOut {
		t.Fatalf("expected timed_out=true: %+v", res)
	}
	if res.Success {
		t.Fatalf("timed-out run must fail: %+v", res)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("timed-out tree run took too long: %v", elapsed)
	}
}

// StartReaper must be safe to call from every entrypoint.
func TestStartReaperIdempotent(t *testing.T) {
	StartReaper()
	StartReaper()
}
