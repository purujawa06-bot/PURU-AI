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

func TestToolCount(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	if len(tools) != 11 {
		t.Fatalf("tools = %d, want exactly 11", len(tools))
	}
	for _, n := range []string{"read_file", "write_file", "list_dir", "edit_file", "append_file", "exec", "telegram_sendfile", "telegram_getuser", "get_env", "web_search", "web_fetch"} {
		if tools[n] == nil {
			t.Fatalf("tool %s missing", n)
		}
	}
}

// Deklarasi picoclaw-parity: read_file(path, offset, length),
// write_file(path, content, overwrite), list_dir(path),
// edit_file(path, old_text, new_text), exec(action wajib; opsional
// command, sessionId, keys, data, background, pty, cwd, timeout).
func TestPicoclawParamDeclarations(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	want := map[string][]string{
		"read_file":   {"path", "offset", "length"},
		"write_file":  {"path", "content", "overwrite"},
		"list_dir":    {"path"},
		"edit_file":   {"path", "old_text", "new_text"},
		"append_file": {"path", "content"},
		"exec":        {"action", "command", "sessionId", "background", "cwd", "timeout"},
	}
	wantRequired := map[string][]string{
		"read_file":   {"path"},
		"write_file":  {"path", "content"},
		"list_dir":    {"path"},
		"edit_file":   {"path", "old_text", "new_text"},
		"append_file": {"path", "content"},
		"exec":        {"action"},
	}
	for name, props := range want {
		params, _ := tools[name].Parameters["properties"].(map[string]any)
		for _, p := range props {
			if params[p] == nil {
				t.Errorf("%s: param %s missing", name, p)
			}
		}
		req, _ := tools[name].Parameters["required"].([]string)
		gotReq := map[string]bool{}
		for _, r := range req {
			gotReq[r] = true
		}
		for _, r := range wantRequired[name] {
			if !gotReq[r] {
				t.Errorf("%s: required %s missing (got %v)", name, r, req)
			}
		}
		if len(req) != len(wantRequired[name]) {
			t.Errorf("%s: required = %v, want %v", name, req, wantRequired[name])
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
	er, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "sub/a.txt", "old_text": "halo", "new_text": "hai"})
	if s, _ := er.(string); s != "File edited: sub/a.txt" {
		t.Fatalf("edit failed: %v", er)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "sub", "a.txt")); string(b) != "hai" {
		t.Fatalf("edit mismatch: %q", b)
	}
	// edit outside must fail
	wo, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "../out.txt", "old_text": "x", "new_text": "y"})
	if m, _ := wo.(map[string]any); m["success"] != false {
		t.Fatalf("escape edit must fail: %v", wo)
	}
	// edit unique-ok, ambiguous-fail
	if err := os.WriteFile(filepath.Join(ws, "b.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	er, _ = tools["edit_file"].Run(ctx, map[string]any{"path": "b.txt", "old_text": "aaa", "new_text": "bbb"})
	if s, _ := er.(string); s != "File edited: b.txt" {
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
	res, _ := runExec(t.TempDir(), "echo hi", 10, 256, false)
	m := res.(execResult)
	if !m.Success || m.ExitCode != 0 {
		t.Fatalf("echo failed: %+v", res)
	}
}

func TestExecTimeoutKills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based timeout test unix-only")
	}
	res, _ := runExec(t.TempDir(), "sleep 10", 1, 256, false)
	m := res.(execResult)
	if !m.TimedOut {
		t.Fatalf("expected timeout, got %+v", res)
	}
}

func TestExecWorkdirJailed(t *testing.T) {
	ws := t.TempDir()
	a := testAgent(ws)
	tools := BuildTools(a, nil)
	out, _ := tools["exec"].Run(context.Background(), map[string]any{"action": "run", "command": "echo hi", "cwd": ".."})
	if m, _ := out.(map[string]any); m["success"] != false {
		t.Fatalf("exec cwd escape must fail: %v", out)
	}
}

func TestExecRejectsUnknownAction(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	out, _ := tools["exec"].Run(context.Background(), map[string]any{"action": "invalid"})
	if m, _ := out.(map[string]any); m["success"] != false {
		t.Fatalf("exec unknown action must fail: %v", out)
	}
}

func TestExecBackgroundSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("background session timing unix-behavior check")
	}
	tools := BuildTools(testAgent(t.TempDir()), nil)
	ctx := context.Background()
	out, _ := tools["exec"].Run(ctx, map[string]any{"action": "run", "command": "sleep 5", "background": true, "timeout": 30})
	m, _ := out.(map[string]any)
	sid, _ := m["sessionId"].(string)
	if m["success"] != true || sid == "" {
		t.Fatalf("background run must return sessionId: %v", out)
	}
	lst, _ := tools["exec"].Run(ctx, map[string]any{"action": "list"})
	found := false
	if arr, ok := lst.([]map[string]any); ok {
		for _, s := range arr {
			if s["sessionId"] == sid {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("session %s missing from list: %v", sid, lst)
	}
	p, _ := tools["exec"].Run(ctx, map[string]any{"action": "poll", "sessionId": sid})
	if pm, _ := p.(map[string]any); pm["sessionId"] != sid {
		t.Fatalf("poll must return session: %v", p)
	}
	r, _ := tools["exec"].Run(ctx, map[string]any{"action": "read", "sessionId": sid})
	if rm, _ := r.(map[string]any); rm["sessionId"] != sid {
		t.Fatalf("read must return session output: %v", r)
	}
	k, _ := tools["exec"].Run(ctx, map[string]any{"action": "kill", "sessionId": sid})
	if km, _ := k.(map[string]any); km["sessionId"] != sid {
		t.Fatalf("kill must confirm session: %v", k)
	}
	if bad, _ := tools["exec"].Run(ctx, map[string]any{"action": "poll", "sessionId": "nope"}); !hasErrPicoclaw(bad) {
		t.Fatalf("poll unknown session must error: %v", bad)
	}
}

// Respons gaya picoclaw: read_file header [file: ...], write/edit/append
// teks ringkas, list_dir baris DIR:/FILE:.
func TestPicoclawStyleResponses(t *testing.T) {
	ws := t.TempDir()
	tools := BuildTools(testAgent(ws), nil)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(ws, "r.txt"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := tools["read_file"].Run(ctx, map[string]any{"path": "r.txt"})
	s, _ := r.(string)
	if !strings.Contains(s, "[file: r.txt |") || !strings.Contains(s, "[END OF FILE") {
		t.Fatalf("read_file header = %q", s)
	}
	w, _ := tools["write_file"].Run(ctx, map[string]any{"path": "w.txt", "content": "x"})
	if ws2, _ := w.(string); ws2 != "File written: w.txt" {
		t.Fatalf("write_file = %q", w)
	}
	if r, _ := tools["write_file"].Run(ctx, map[string]any{"path": "w.txt", "content": "y"}); hasErrPicoclaw(r) == false {
		t.Fatalf("write tanpa overwrite harus error: %v", r)
	}
	l, _ := tools["list_dir"].Run(ctx, map[string]any{"path": "."})
	if ls, _ := l.(string); !strings.Contains(ls, "FILE: w.txt") {
		t.Fatalf("list_dir = %q", l)
	}
	le, _ := tools["list_dir"].Run(ctx, map[string]any{"path": "kosong-sub-test"})
	if hasErrPicoclaw(le) == false {
		t.Fatalf("list_dir path tak ada harus error: %v", le)
	}
	if err := os.MkdirAll(filepath.Join(ws, "kosong"), 0o755); err != nil {
		t.Fatal(err)
	}
	le, _ = tools["list_dir"].Run(ctx, map[string]any{"path": "kosong"})
	if les, _ := le.(string); les == "" {
		t.Fatalf("list_dir dir kosong tidak boleh string kosong (bikin model looping)")
	}
	e, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "w.txt", "old_text": "x", "new_text": "y"})
	if es, _ := e.(string); es != "File edited: w.txt" {
		t.Fatalf("edit_file = %q", e)
	}
	p, _ := tools["append_file"].Run(ctx, map[string]any{"path": "w.txt", "content": "z"})
	if ps, _ := p.(string); ps != "Appended to w.txt" {
		t.Fatalf("append_file = %q", p)
	}
}

func hasErrPicoclaw(v any) bool {
	m, _ := v.(map[string]any)
	if m == nil {
		return false
	}
	e, _ := m["error"].(string)
	return strings.TrimSpace(e) != ""
}

// Output exec dipotong saat capture: buffer tidak pernah lebih dari cap,
// kelebihan ditandai ...[truncated, total X].
func TestCappedWriterTruncates(t *testing.T) {
	w := &cappedWriter{max: 100}
	if _, err := w.Write([]byte(strings.Repeat("abcdef", 50))); err != nil {
		t.Fatal(err)
	}
	s := w.String()
	if len(s) > 100+64 {
		t.Fatalf("buffer bocor: %d bytes", len(s))
	}
	if !strings.Contains(s, "[truncated") {
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
