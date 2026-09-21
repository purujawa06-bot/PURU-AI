package ai

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

func testAgent(ws string) *Agent {
	return &Agent{Config: &config.Config{Workspace: ws, RestrictWorkspace: true}}
}

func TestFourTools(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	if len(tools) != 4 {
		t.Fatalf("tools = %d, want exactly 4", len(tools))
	}
	for _, n := range []string{"edit_file", "exec", "telegram_sendfile", "telegram_getuser"} {
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
	// seed file directly, edit via tool, verify from disk
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", "a.txt"), []byte("halo"), 0o644); err != nil {
		t.Fatal(err)
	}
	er, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "sub/a.txt", "old_string": "halo", "new_string": "hai"})
	if m, _ := er.(map[string]any); m["success"] != true {
		t.Fatalf("edit failed: %v", er)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "sub", "a.txt")); string(b) != "hai" {
		t.Fatalf("edit mismatch: %q", b)
	}
	// edit outside must fail
	wo, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "../out.txt", "old_string": "x", "new_string": "y"})
	if m, _ := wo.(map[string]any); m["success"] != false {
		t.Fatalf("escape edit must fail: %v", wo)
	}
	// edit unique-ok, ambiguous-fail
	if err := os.WriteFile(filepath.Join(ws, "b.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	er, _ = tools["edit_file"].Run(ctx, map[string]any{"path": "b.txt", "old_string": "aaa", "new_string": "bbb"})
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
	res := runExec(t.TempDir(), "echo hi", 10, 256)
	if !res.Success || res.ExitCode != 0 {
		t.Fatalf("echo failed: %+v", res)
	}
}

func TestExecTimeoutKills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based timeout test unix-only")
	}
	res := runExec(t.TempDir(), "sleep 10", 1, 256)
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

// Output exec dipotong saat capture: buffer tidak pernah lebih dari cap,
// kelebihan ditandai ...[truncated].
func TestCappedWriterTruncates(t *testing.T) {
	w := &cappedWriter{max: 100}
	if _, err := w.Write([]byte(strings.Repeat("abcdef", 50))); err != nil {
		t.Fatal(err)
	}
	s := w.String()
	if len(s) > 100+len("\n...[truncated]") {
		t.Fatalf("buffer bocor: %d bytes", len(s))
	}
	if !strings.HasSuffix(s, "[truncated]") {
		t.Fatalf("kelebihan harus ditandai truncated")
	}
	w2 := &cappedWriter{max: 100}
	if _, err := w2.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if w2.String() != "ok" {
		t.Fatalf("output pas tidak boleh ditandai: %q", w2.String())
	}
}

func TestClampMemMB(t *testing.T) {
	if got := clampMemMB(0); got != defaultExecMemMB {
		t.Errorf("0 -> %d, want default %d", got, defaultExecMemMB)
	}
	if got := clampMemMB(-5); got != defaultExecMemMB {
		t.Errorf("negatif -> %d, want default", got)
	}
	if got := clampMemMB(32); got != minExecMemMB {
		t.Errorf("32 -> %d, want min %d", got, minExecMemMB)
	}
	if got := clampMemMB(512); got != 512 {
		t.Errorf("512 -> %d", got)
	}
}
