package ai

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The exec session map must not grow unbounded: the oldest finished
// sessions are evicted beyond maxExecSessions; running ones are never evicted.
func TestPruneSessionsCapsFinished(t *testing.T) {
	sessionsMu.Lock()
	saved := sessions
	sessions = make(map[string]*execSession)
	defer func() { sessions = saved; sessionsMu.Unlock() }()

	base := time.Now().Add(-time.Hour)
	for i := 0; i < maxExecSessions+5; i++ {
		id := fmt.Sprintf("sess_prune_%03d", i)
		sessions[id] = &execSession{ID: id, StartTime: base.Add(time.Duration(i) * time.Second)}
	}
	sessions["sess_prune_running"] = &execSession{ID: "sess_prune_running", StartTime: base, IsRunning: true}

	pruneSessionsLocked()

	if len(sessions) > maxExecSessions {
		t.Fatalf("sesi = %d, want <= %d", len(sessions), maxExecSessions)
	}
	if _, ok := sessions["sess_prune_running"]; !ok {
		t.Fatalf("running session must not be evicted")
	}
	if _, ok := sessions["sess_prune_000"]; ok {
		t.Fatalf("oldest finished session should be evicted first")
	}
}

// exec read must carry the same diagnosis fields as poll: status,
// timed_out, memory_limited — so the AI knows why a process stopped
// without a second call.
func TestReadExecCarriesStatusFlags(t *testing.T) {
	sessionsMu.Lock()
	saved := sessions
	sessions = make(map[string]*execSession)
	sessionsMu.Unlock()
	defer func() {
		sessionsMu.Lock()
		sessions = saved
		sessionsMu.Unlock()
	}()

	sessionsMu.Lock()
	sessions["sess_read_flags"] = &execSession{
		ID:            "sess_read_flags",
		Output:        &cappedWriter{max: maxExecOutput},
		StartTime:     time.Now(),
		IsRunning:     false,
		TimedOut:      true,
		MemoryLimited: true,
	}
	sessionsMu.Unlock()

	got, err := readExec("sess_read_flags")
	if err != nil {
		t.Fatalf("readExec: %v", err)
	}
	m, _ := got.(map[string]any)
	if m["status"] != "finished" {
		t.Fatalf("status = %v, want finished", m["status"])
	}
	if m["timed_out"] != true || m["memory_limited"] != true {
		t.Fatalf("flag diagnosis hilang: %v", m)
	}
	if _, ok := m["output"]; !ok {
		t.Fatalf("output must be kept: %v", m)
	}
	if _, err := readExec("sess_tidak_ada"); err == nil {
		t.Fatalf("read on unknown session should error")
	}
}

// killExec must mark the session finished immediately: without this a
// killed session stays running=true until the cmd.Wait() watcher fires,
// misleading poll/read/list during that gap.
func TestKillExecMarksFinishedImmediately(t *testing.T) {
	sessionsMu.Lock()
	saved := sessions
	sessions = make(map[string]*execSession)
	sessionsMu.Unlock()
	defer func() {
		sessionsMu.Lock()
		sessions = saved
		sessionsMu.Unlock()
	}()

	_, cancel := context.WithCancel(context.Background())
	sessionsMu.Lock()
	sessions["sess_kill_now"] = &execSession{
		ID:        "sess_kill_now",
		Output:    &cappedWriter{max: maxExecOutput},
		Cancel:    cancel,
		StartTime: time.Now(),
		IsRunning: true,
	}
	sessionsMu.Unlock()

	got, err := killExec("sess_kill_now")
	if err != nil {
		t.Fatalf("killExec: %v", err)
	}
	m, _ := got.(map[string]any)
	if m["status"] != "killed" {
		t.Fatalf("status = %v, want killed", m["status"])
	}
	sess, _ := lookupSession("sess_kill_now")
	if running, _, _, _ := sess.snapshot(); running {
		t.Fatalf("killed session must be finished immediately (running=false)")
	}

	// An already-finished session must not be reported as killed again.
	got2, err := killExec("sess_kill_now")
	if err != nil {
		t.Fatalf("killExec kedua: %v", err)
	}
	m2, _ := got2.(map[string]any)
	if m2["status"] != "already finished" {
		t.Fatalf("status = %v, want already finished", m2["status"])
	}
	if _, err := killExec("sess_tidak_ada"); err == nil {
		t.Fatalf("kill on unknown session should error")
	}
}
