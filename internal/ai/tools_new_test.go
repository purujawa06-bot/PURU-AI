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

// Setiap tool harus punya schema parameters yang valid JSON object (provider
// OpenAI-compatible strict menolak properties:null / parameters null).
func TestToolSchemasValid(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	for name, tool := range tools {
		if tool == nil {
			t.Fatalf("tool %s nil", name)
		}
		if tool.Parameters == nil {
			t.Errorf("%s: parameters nil", name)
			continue
		}
		if typ, _ := tool.Parameters["type"].(string); typ != "object" {
			t.Errorf("%s: type = %v, want object", name, tool.Parameters["type"])
		}
		if props, _ := tool.Parameters["properties"].(map[string]any); props == nil {
			t.Errorf("%s: properties nil (schema: %v)", name, tool.Parameters)
		}
	}
	for _, fn := range toFunctionDefinitions(tools) {
		params, ok := fn.Parameters.(map[string]any)
		if !ok || params == nil {
			t.Errorf("function %s: parameters = %v, want object", fn.Name, fn.Parameters)
			continue
		}
		if props, _ := params["properties"].(map[string]any); props == nil {
			t.Errorf("function %s: properties nil", fn.Name)
		}
	}
}

func TestToolCount(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	if len(tools) != 15 {
		t.Fatalf("tools = %d, want exactly 15 (web_search opt-in)", len(tools))
	}
	for _, n := range []string{"read_file", "write_file", "list_dir", "grep", "edit_file", "append_file", "run_shell_command", "telegram_sendfile", "telegram_getuser", "get_env", "web_fetch", "manage_schedule", "spawn_agent", "use_skill", "stop_skill"} {
		if tools[n] == nil {
			t.Fatalf("tool %s missing", n)
		}
	}
	if tools["web_search"] != nil {
		t.Fatalf("web_search must be absent by default (opt-in via web_search.aistudio)")
	}
	if tools["edit_file_replace_string"] != nil || tools["edit_file_replace_line"] != nil || tools["edit_file_apply_patch"] != nil {
		t.Fatalf("old edit tools must be gone (use single edit_file)")
	}
	// Opt-in: active aistudio adds web_search as the 16th tool.
	searchAgent := testAgent(t.TempDir())
	searchAgent.Config.WebSearch.AIStudio.Active = true
	searchAgent.Config.WebSearch.AIStudio.Model = "gemini-2.5-flash"
	searchAgent.Config.WebSearch.AIStudio.APIKey = "test-key"
	enabled := BuildTools(searchAgent, nil)
	if len(enabled) != 16 {
		t.Fatalf("enabled tools = %d, want 16", len(enabled))
	}
	if enabled["web_search"] == nil {
		t.Fatalf("web_search missing when aistudio active")
	}
}

// Deklarasi tools: read_file(path, start_line, length),
// write_file(path, content, overwrite), list_dir(path),
// edit_file(path, old_string, new_string),
// spawn_agent(agent_name, system_prompt, task_prompt, agent_read, agent_write, agent_exec, agent_search),
// run_shell_command(action wajib; opsional command, sessionId, background, cwd, timeout).
func TestPicoclawParamDeclarations(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	want := map[string][]string{
		"read_file":   {"path", "start_line", "length"},
		"write_file":  {"path", "content", "overwrite"},
		"list_dir":    {"path"},
		"grep":        {"path", "keyword", "ext", "limit"},
		"edit_file":   {"path", "old_string", "new_string"},
		"spawn_agent": {"agent_name", "system_prompt", "task_prompt", "agent_read", "agent_write", "agent_exec", "agent_search"},
		"append_file": {"path", "content"},
		"run_shell_command":        {"action", "command", "sessionId", "background", "cwd", "timeout"},
	}
	wantRequired := map[string][]string{
		"read_file":   {"path"},
		"write_file":  {"path", "content"},
		"list_dir":    {"path"},
		"grep":        {"path", "keyword"},
		"edit_file":   {"path", "old_string", "new_string"},
		"spawn_agent": {"agent_name", "system_prompt", "task_prompt"},
		"append_file": {"path", "content"},
		"run_shell_command":        {"action"},
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

	if _, err := resolvePath(ws, true, "#cwd/../escape.txt"); err == nil {
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
	er, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "#cwd/sub/a.txt", "old_string": "halo", "new_string": "hai"})
	if s, _ := er.(string); s != "File edited: #cwd/sub/a.txt" {
		t.Fatalf("edit failed: %v", er)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "sub", "a.txt")); string(b) != "hai" {
		t.Fatalf("edit mismatch: %q", b)
	}

	// fuzzy/line-based match test
	if err := os.WriteFile(filepath.Join(ws, "fuzzy.txt"), []byte("line1\n  line2  \nline3"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Try editing with different spacing/newline
	er, _ = tools["edit_file"].Run(ctx, map[string]any{
		"path":     "#cwd/fuzzy.txt",
		"old_string": "line1\nline2", // missing spaces and different newline handling
		"new_string": "replaced",
	})
	if s, _ := er.(string); s != "File edited: #cwd/fuzzy.txt" {
		t.Fatalf("fuzzy edit failed: %v", er)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "fuzzy.txt")); !strings.HasPrefix(string(b), "replaced\nline3") {
		t.Fatalf("fuzzy result mismatch: %q", b)
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
	er, _ = tools["edit_file"].Run(ctx, map[string]any{"path": "#cwd/b.txt", "old_string": "aaa", "new_string": "bbb"})
	if s, _ := er.(string); s != "File edited: #cwd/b.txt" {
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
	res, _ := runExec(context.Background(), t.TempDir(), "echo hi", 10, 256, false)
	m := res.(execResult)
	if !m.Success || m.ExitCode != 0 {
		t.Fatalf("echo failed: %+v", res)
	}
}

func TestExecTimeoutKills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep-based timeout test unix-only")
	}
	res, _ := runExec(context.Background(), t.TempDir(), "sleep 10", 1, 256, false)
	m := res.(execResult)
	if !m.TimedOut {
		t.Fatalf("expected timeout, got %+v", res)
	}
}

func TestExecWorkdirJailed(t *testing.T) {
	ws := t.TempDir()
	a := testAgent(ws)
	tools := BuildTools(a, nil)
	out, _ := tools["run_shell_command"].Run(context.Background(), map[string]any{"action": "run", "command": "echo hi", "cwd": ".."})
	if m, _ := out.(map[string]any); m["success"] != false {
		t.Fatalf("run_shell_command cwd escape must fail: %v", out)
	}
}

func TestExecRejectsUnknownAction(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	out, _ := tools["run_shell_command"].Run(context.Background(), map[string]any{"action": "invalid"})
	if m, _ := out.(map[string]any); m["success"] != false {
		t.Fatalf("run_shell_command unknown action must fail: %v", out)
	}
}

func TestExecBackgroundSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("background session timing unix-behavior check")
	}
	tools := BuildTools(testAgent(t.TempDir()), nil)
	ctx := context.Background()
	out, _ := tools["run_shell_command"].Run(ctx, map[string]any{"action": "run", "command": "sleep 5", "background": true, "timeout": 30})
	m, _ := out.(map[string]any)
	sid, _ := m["sessionId"].(string)
	if m["success"] != true || sid == "" {
		t.Fatalf("background run must return sessionId: %v", out)
	}
	lst, _ := tools["run_shell_command"].Run(ctx, map[string]any{"action": "list"})
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
	p, _ := tools["run_shell_command"].Run(ctx, map[string]any{"action": "poll", "sessionId": sid})
	if pm, _ := p.(map[string]any); pm["sessionId"] != sid {
		t.Fatalf("poll must return session: %v", p)
	}
	r, _ := tools["run_shell_command"].Run(ctx, map[string]any{"action": "read", "sessionId": sid})
	if rm, _ := r.(map[string]any); rm["sessionId"] != sid {
		t.Fatalf("read must return session output: %v", r)
	}
	k, _ := tools["run_shell_command"].Run(ctx, map[string]any{"action": "kill", "sessionId": sid})
	if km, _ := k.(map[string]any); km["sessionId"] != sid {
		t.Fatalf("kill must confirm session: %v", k)
	}
	if bad, _ := tools["run_shell_command"].Run(ctx, map[string]any{"action": "poll", "sessionId": "nope"}); !hasErrPicoclaw(bad) {
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
	r, _ := tools["read_file"].Run(ctx, map[string]any{"path": "#cwd/r.txt"})
	s, _ := r.(string)
	if !strings.Contains(s, "[file: r.txt |") || !strings.Contains(s, "[END OF FILE") {
		t.Fatalf("read_file header = %q", s)
	}
	w, _ := tools["write_file"].Run(ctx, map[string]any{"path": "#cwd/w.txt", "content": "x"})
	if ws2, _ := w.(string); ws2 != "File written: #cwd/w.txt" {
		t.Fatalf("write_file = %q", w)
	}
	if r, _ := tools["write_file"].Run(ctx, map[string]any{"path": "#cwd/w.txt", "content": "y"}); hasErrPicoclaw(r) == false {
		t.Fatalf("write tanpa overwrite harus error: %v", r)
	}
	l, _ := tools["list_dir"].Run(ctx, map[string]any{"path": "#cwd"})
	if ls, _ := l.(string); !strings.Contains(ls, "FILE: w.txt") {
		t.Fatalf("list_dir = %q", l)
	}
	le, _ := tools["list_dir"].Run(ctx, map[string]any{"path": "#cwd/kosong-sub-test"})
	if hasErrPicoclaw(le) == false {
		t.Fatalf("list_dir path tak ada harus error: %v", le)
	}
	if err := os.MkdirAll(filepath.Join(ws, "kosong"), 0o755); err != nil {
		t.Fatal(err)
	}
	le, _ = tools["list_dir"].Run(ctx, map[string]any{"path": "#cwd/kosong"})
	if les, _ := le.(string); les == "" {
		t.Fatalf("list_dir dir kosong tidak boleh string kosong (bikin model looping)")
	}
	e, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "#cwd/w.txt", "old_string": "x", "new_string": "y"})
	if es, _ := e.(string); es != "File edited: #cwd/w.txt" {
		t.Fatalf("edit_file = %q", e)
	}
	p, _ := tools["append_file"].Run(ctx, map[string]any{"path": "#cwd/w.txt", "content": "z"})
	if ps, _ := p.(string); ps != "Appended to #cwd/w.txt" {
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
// Single edit_file(path, old_string, new_string): unique-ok, ambiguous-fail,
// missing-fail, jail-escape-fail.
func TestEditSingle(t *testing.T) {
	ws := t.TempDir()
	tools := BuildTools(testAgent(ws), nil)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("l1\nl2\nl3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "#cwd/a.txt", "old_string": "l2", "new_string": "L2"}); r.(string) != "File edited: #cwd/a.txt" {
		t.Fatalf("edit: %v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(b) != "l1\nL2\nl3\n" {
		t.Fatalf("mismatch: %q", b)
	}
	// ambiguous must fail
	if err := os.WriteFile(filepath.Join(ws, "b.txt"), []byte("x\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "#cwd/b.txt", "old_string": "x", "new_string": "y"}); !hasErrPicoclaw(r) {
		t.Fatalf("ambiguous must fail: %v", r)
	}
	// missing must fail
	if r, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "#cwd/a.txt", "old_string": "nope", "new_string": "y"}); !hasErrPicoclaw(r) {
		t.Fatalf("missing must fail: %v", r)
	}
	// jail escape must fail
	if r, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "../x.txt", "old_string": "x", "new_string": "y"}); !hasErrPicoclaw(r) {
		t.Fatalf("escape must fail: %v", r)
	}
	// empty old_string must fail
	if r, _ := tools["edit_file"].Run(ctx, map[string]any{"path": "#cwd/a.txt", "old_string": "", "new_string": "y"}); !hasErrPicoclaw(r) {
		t.Fatalf("empty old_string must fail: %v", r)
	}
}

func TestSpawnAgentSchema(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	sp := tools["spawn_agent"]
	if sp == nil {
		t.Fatalf("spawn_agent missing")
	}
	params, _ := sp.Parameters["properties"].(map[string]any)
	for _, p := range []string{"agent_name", "system_prompt", "task_prompt", "agent_read", "agent_write", "agent_exec", "agent_search"} {
		if params[p] == nil {
			t.Errorf("spawn_agent param %s missing", p)
		}
		if typ, _ := params[p].(map[string]any)["type"].(string); p != "agent_name" && p != "system_prompt" && p != "task_prompt" && typ != "boolean" {
			t.Errorf("spawn_agent param %s type = %v, want boolean", p, typ)
		}
	}
	if params["allowed_tools"] != nil {
		t.Errorf("spawn_agent must not have allowed_tools anymore")
	}
	req, _ := sp.Parameters["required"].([]string)
	got := map[string]bool{}
	for _, r := range req {
		got[r] = true
	}
	for _, r := range []string{"agent_name", "system_prompt", "task_prompt"} {
		if !got[r] {
			t.Errorf("spawn_agent required %s missing (got %v)", r, req)
		}
	}
}

func TestSpawnAgentValidation(t *testing.T) {
	a := testAgent(t.TempDir())
	tools := BuildTools(a, nil)
	ctx := context.Background()
	// no model -> unavailable (no panic)
	if r, _ := tools["spawn_agent"].Run(ctx, map[string]any{"agent_name": "r1", "system_prompt": "s", "task_prompt": "t"}); !hasErrPicoclaw(r) {
		t.Fatalf("no-model spawn must fail: %v", r)
	}
	// empty agent_name must fail even without model check order: model nil first, so use fake client path?
	// filterSpawnTools: no flags/all-false = all except spawn_agent; each
	// true flag adds its group (union, combinable); skill tools always on.
	full := map[string]*Tool{
		"read_file":        {Name: "read_file"},
		"list_dir":         {Name: "list_dir"},
		"grep":             {Name: "grep"},
		"get_env":          {Name: "get_env"},
		"telegram_getuser": {Name: "telegram_getuser"},
		"write_file":       {Name: "write_file"},
		"edit_file":        {Name: "edit_file"},
		"append_file":      {Name: "append_file"},
		"telegram_sendfile": {Name: "telegram_sendfile"},
		"run_shell_command":             {Name: "run_shell_command"},
		"manage_schedule":  {Name: "manage_schedule"},
		"web_fetch":        {Name: "web_fetch"},
		"web_search":       {Name: "web_search"},
		"use_skill":        {Name: "use_skill"},
		"stop_skill":       {Name: "stop_skill"},
		"spawn_agent":      {Name: "spawn_agent"},
	}
	got := filterSpawnTools(full, nil)
	if len(got) != 15 || got["spawn_agent"] != nil {
		t.Fatalf("nil args must give all except spawn_agent: %v", got)
	}
	got = filterSpawnTools(full, map[string]any{})
	if len(got) != 15 || got["spawn_agent"] != nil {
		t.Fatalf("empty args must give all except spawn_agent: %v", got)
	}
	got = filterSpawnTools(full, map[string]any{"agent_read": false, "agent_write": false, "agent_exec": false, "agent_search": false})
	if len(got) != 15 || got["spawn_agent"] != nil {
		t.Fatalf("all-false must give all except spawn_agent: %v", got)
	}
	// read only: 5 read + 2 skill
	got = filterSpawnTools(full, map[string]any{"agent_read": true})
	for _, n := range []string{"read_file", "list_dir", "grep", "get_env", "telegram_getuser", "use_skill", "stop_skill"} {
		if got[n] == nil {
			t.Fatalf("agent_read must include %s: %v", n, got)
		}
	}
	if len(got) != 7 {
		t.Fatalf("agent_read = %v, want 7 tools", got)
	}
	// write only: 4 write + 2 skill
	got = filterSpawnTools(full, map[string]any{"agent_write": true})
	if len(got) != 6 || got["write_file"] == nil || got["edit_file"] == nil || got["append_file"] == nil || got["telegram_sendfile"] == nil {
		t.Fatalf("agent_write = %v, want 4 write + 2 skill", got)
	}
	// exec only: 2 exec + 2 skill
	got = filterSpawnTools(full, map[string]any{"agent_exec": true})
	if len(got) != 4 || got["run_shell_command"] == nil || got["manage_schedule"] == nil {
		t.Fatalf("agent_exec = %v, want run_shell_command+manage_schedule+2 skill", got)
	}
	// search only: 2 search + 2 skill
	got = filterSpawnTools(full, map[string]any{"agent_search": true})
	if len(got) != 4 || got["web_fetch"] == nil || got["web_search"] == nil {
		t.Fatalf("agent_search = %v, want web_fetch+web_search+2 skill", got)
	}
	// combination read+search: 5+2+2 skill = 9, never spawn_agent
	got = filterSpawnTools(full, map[string]any{"agent_read": true, "agent_search": true})
	if len(got) != 9 || got["spawn_agent"] != nil || got["read_file"] == nil || got["grep"] == nil || got["web_fetch"] == nil || got["use_skill"] == nil {
		t.Fatalf("read+search combo = %v, want 9 tools", got)
	}
	// all true: everything except spawn_agent
	got = filterSpawnTools(full, map[string]any{"agent_read": true, "agent_write": true, "agent_exec": true, "agent_search": true})
	if len(got) != 15 || got["spawn_agent"] != nil {
		t.Fatalf("all-true must give all except spawn_agent: %v", got)
	}
	// missing tool in parent is skipped, not error: search without web_search
	noSearch := map[string]*Tool{
		"web_fetch": {Name: "web_fetch"},
		"use_skill": {Name: "use_skill"},
	}
	got = filterSpawnTools(noSearch, map[string]any{"agent_search": true})
	if len(got) != 2 || got["web_fetch"] == nil || got["use_skill"] == nil {
		t.Fatalf("search without web_search parent = %v, want web_fetch+use_skill", got)
	}
}
