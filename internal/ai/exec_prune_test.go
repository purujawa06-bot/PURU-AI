package ai

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Map sesi exec tidak boleh tumbuh tanpa batas: sesi selesai terlama
// di-evict bila melebihi maxExecSessions, sesi running tak pernah di-evict.
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
		t.Fatalf("sesi running tidak boleh di-evict")
	}
	if _, ok := sessions["sess_prune_000"]; ok {
		t.Fatalf("sesi selesai terlama harus di-evict lebih dulu")
	}
}

// exec read harus membawa status diagnosis yang sama dengan poll: status,
// timed_out, memory_limited — agar AI tahu kenapa proses berhenti tanpa
// dua kali panggil.
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
		t.Fatalf("output harus tetap ada: %v", m)
	}
	if _, err := readExec("sess_tidak_ada"); err == nil {
		t.Fatalf("read sesi tak dikenal harus error")
	}
}

// killExec harus langsung menandai sesi selesai: tanpa ini sesi killed tetap
// running=true sampai watcher cmd.Wait() jalan, sehingga poll/read/list
// menyesatkan di jeda tersebut.
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
		t.Fatalf("sesi killed harus langsung finished (running=false)")
	}

	// Sesi yang sudah selesai tak boleh dilaporkan killed ulang.
	got2, err := killExec("sess_kill_now")
	if err != nil {
		t.Fatalf("killExec kedua: %v", err)
	}
	m2, _ := got2.(map[string]any)
	if m2["status"] != "already finished" {
		t.Fatalf("status = %v, want already finished", m2["status"])
	}
	if _, err := killExec("sess_tidak_ada"); err == nil {
		t.Fatalf("kill sesi tak dikenal harus error")
	}
}
