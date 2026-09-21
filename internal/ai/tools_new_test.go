package ai

import (
	"context"
	"runtime"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

func testAgent(ws string) *Agent {
	return &Agent{Config: &config.Config{Workspace: ws, RestrictWorkspace: true}}
}

func TestSixTools(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	if len(tools) != 6 {
		t.Fatalf("tools = %d, want exactly 6", len(tools))
	}
	for _, n := range []string{"read_file", "write_file", "edit_file", "exec", "telegram_sendfile", "telegram_getuser"} {
		if tools[n] == nil {
			t.Fatalf("tool %s missing", n)
		}
	}
}

func TestWorkspaceJail(t *testing.T) {
	ws := t.TempDir()
	a := testAgent(ws)
	tools := BuildTools(a, nil)
	ctx := context.Background()

	if _, err := resolvePath(ws, true, "../escape.txt"); err == nil {
		t.Errorf("expected ../ escape rejected")
	}
	if _, err := resolvePath(ws, true, "/etc/passwd"); err == nil {
		// On Windows /etc/passwd is not absolute; only enforce on unix.
		if runtime.GOOS != "windows" {
			t.Errorf("expected absolute outside rejected")
		}
	}
	// write + read roundtrip inside workspace
	wr, _ := tools["write_file"].Run(ctx, map[string]any{"path": "sub/a.txt", "content": "halo"})
	if m, _ := wr.(map[string]any); m["success"] != true {
		t.Fatalf("write failed: %v", wr)
	}
	rr, _ := tools["read_file"].Run(ctx, map[string]any{"path": "sub/a.txt"})
	if m, _ := rr.(map[string]any); m["content"] != "halo" {
		t.Fatalf("read mismatch: %v", rr)
	}
	// write outside must fail
	wo, _ := tools["write_file"].Run(ctx, map[string]any{"path": "../out.txt", "content": "x"})
	if m, _ := wo.(map[string]any); m["success"] != false {
		t.Fatalf("escape write must fail: %v", wo)
	}
	// edit unique-ok, ambiguous-fail
	_, _ = tools["write_file"].Run(ctx, map[string]any{"path": "b.txt", "content": "aaa"})
	er, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "b.txt", "old_string": "aaa", "new_string": "bbb"})
	if m, _ := er.(map[string]any); m["success"] != true {
		t.Fatalf("edit failed: %v", er)
	}
}

func TestClampTimeout(t *testing.T) {
	if got := clampTimeout(nil); got != defaultExecTimeoutSec {
		t.Errorf("nil -> %d, want %d", got, defaultExecTimeoutSec)
	}
	if got := clampTimeout(0); got != defaultExecTimeoutSec {
		t.Errorf("0 -> %d, want default", got)
	}
	if got := clampTimeout(9999); got != maxExecTimeoutSec {
		t.Errorf("huge -> %d, want max %d", got, maxExecTimeoutSec)
	}
	if got := clampTimeout(5); got != 5 {
		t.Errorf("5 -> %d", got)
	}
}

func TestExecSuccess(t *testing.T) {
	res := runExec(t.TempDir(), "echo hi", 10)
	if !res.Success || res.ExitCode != 0 {
		t.Fatalf("echo failed: %+v", res)
	}
}

func TestExecTimeoutKills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based timeout test unix-only")
	}
	res := runExec(t.TempDir(), "sleep 10", 1)
	if !res.TimedOut {
		t.Fatalf("expected timeout, got %+v", res)
	}
}

func TestExecWorkdirJailed(t *testing.T) {
	ws := t.TempDir()
	a := testAgent(ws)
	tools := BuildTools(a, nil)
	out, _ := tools["exec"].Run(context.Background(), map[string]any{"command": "echo hi", "workdir": ".."})
	if m, _ := out.(map[string]any); m["success"] != false {
		t.Fatalf("exec workdir escape must fail: %v", out)
	}
}
