package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepSingleFile(t *testing.T) {
	ws := t.TempDir()
	tools := BuildTools(testAgent(ws), nil)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("l1\nhello world\nl3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := tools["grep"].Run(ctx, map[string]any{"path": "#cwd/a.txt", "keyword": "hello"})
	s, _ := out.(string)
	if !strings.Contains(s, "a.txt:2:") || !strings.Contains(s, "hello world") {
		t.Fatalf("single-file grep = %q", s)
	}
}

func TestGrepFolderRecursiveWithExt(t *testing.T) {
	ws := t.TempDir()
	tools := BuildTools(testAgent(ws), nil)
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(ws, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", "deep", "b.go"), []byte("package x\n// BuildTools here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", "c.md"), []byte("BuildTools in md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := tools["grep"].Run(ctx, map[string]any{"path": "#cwd/sub", "keyword": "BuildTools", "ext": "go"})
	s, _ := out.(string)
	if !strings.Contains(s, "b.go:2:") {
		t.Fatalf("folder grep must hit b.go: %q", s)
	}
	if strings.Contains(s, "c.md") {
		t.Fatalf("ext filter must skip c.md: %q", s)
	}
	// no ext filter hits both
	out, _ = tools["grep"].Run(ctx, map[string]any{"path": "#cwd/sub", "keyword": "BuildTools"})
	s, _ = out.(string)
	if !strings.Contains(s, "b.go") || !strings.Contains(s, "c.md") {
		t.Fatalf("unfiltered grep must hit both: %q", s)
	}
}

func TestGrepNoMatchesAndValidation(t *testing.T) {
	ws := t.TempDir()
	tools := BuildTools(testAgent(ws), nil)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := tools["grep"].Run(ctx, map[string]any{"path": "#cwd/a.txt", "keyword": "zzz-nope"})
	if s, _ := out.(string); s != "(no matches)" {
		t.Fatalf("no match must return (no matches): %q", s)
	}
	out, _ = tools["grep"].Run(ctx, map[string]any{"path": "#cwd/a.txt", "keyword": ""})
	if !hasErrPicoclaw(out) {
		t.Fatalf("empty keyword must fail: %v", out)
	}
	out, _ = tools["grep"].Run(ctx, map[string]any{"path": "#cwd/missing", "keyword": "x"})
	if !hasErrPicoclaw(out) {
		t.Fatalf("missing path must fail: %v", out)
	}
}

func TestGrepJailEscape(t *testing.T) {
	ws := t.TempDir()
	tools := BuildTools(testAgent(ws), nil)
	out, _ := tools["grep"].Run(context.Background(), map[string]any{"path": "../out.txt", "keyword": "x"})
	if !hasErrPicoclaw(out) {
		t.Fatalf("escape grep must fail: %v", out)
	}
}

func TestParseGrepExt(t *testing.T) {
	got := parseGrepExt("go")
	if !got[".go"] || len(got) != 1 {
		t.Fatalf("parse go = %v", got)
	}
	got = parseGrepExt(".go,.md")
	if !got[".go"] || !got[".md"] {
		t.Fatalf("parse .go,.md = %v", got)
	}
	if len(parseGrepExt("")) != 0 {
		t.Fatalf("empty ext must give empty set")
	}
}
