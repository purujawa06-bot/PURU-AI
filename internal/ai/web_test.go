package ai

import (
	"context"
	"strings"
	"testing"
)

func TestWebSearchRejectsEmptyQuery(t *testing.T) {
	if err := validateSearchQuery(""); err == nil {
		t.Fatalf("query kosong harus ditolak")
	}
	if err := validateSearchQuery("   "); err == nil {
		t.Fatalf("query whitespace harus ditolak")
	}
	if err := validateSearchQuery("golang"); err != nil {
		t.Fatalf("query valid ditolak: %v", err)
	}
	tools := BuildTools(testAgent(t.TempDir()), nil)
	out, _ := tools["web_search"].Run(context.Background(), map[string]any{"query": ""})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("web_search query kosong harus success=false: %v", out)
	}
}

func TestWebFetchRejectsNonHTTP(t *testing.T) {
	for _, u := range []string{"", "file:///etc/passwd", "ftp://x.com/a", "gopher://x/", "notaurl-no-scheme"} {
		if _, err := validateFetchURL(u); err == nil {
			t.Errorf("url %q harus ditolak", u)
		}
	}
	for _, u := range []string{"http://localhost/x", "http://127.0.0.1/", "http://192.168.1.1/", "http://10.0.0.1/"} {
		if _, err := validateFetchURL(u); err == nil {
			t.Errorf("host lokal %q harus ditolak", u)
		}
	}
	if _, err := validateFetchURL("https://example.com/a?b=1"); err != nil {
		t.Errorf("url publik ditolak: %v", err)
	}
	tools := BuildTools(testAgent(t.TempDir()), nil)
	out, _ := tools["web_fetch"].Run(context.Background(), map[string]any{"url": "file:///etc/passwd"})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("web_fetch file:// harus success=false: %v", out)
	}
}

func TestClampSearchFetch(t *testing.T) {
	if got := clampSearchCount(0); got != 5 {
		t.Errorf("count 0 -> %d, want 5", got)
	}
	if got := clampSearchCount(999); got != 10 {
		t.Errorf("count 999 -> %d, want 10", got)
	}
	if got := clampSearchCount(-3); got != 1 {
		t.Errorf("count -3 -> %d, want 1", got)
	}
	if got := clampSearchCount(3); got != 3 {
		t.Errorf("count 3 -> %d", got)
	}
	if got := clampFetchChars(0); got != 8000 {
		t.Errorf("max_chars 0 -> %d, want 8000", got)
	}
	if got := clampFetchChars(999999); got != 20000 {
		t.Errorf("max_chars huge -> %d, want 20000", got)
	}
	if got := clampFetchChars(10); got != 1000 {
		t.Errorf("max_chars 10 -> %d, want 1000", got)
	}
	if got := clampFetchChars(5000); got != 5000 {
		t.Errorf("max_chars 5000 -> %d", got)
	}
}

func TestStripHTMLToText(t *testing.T) {
	got := stripHTMLToText(`<html><head><style>a{}</style></head><body><script>x()</script><h1>Halo &amp; Hai</h1><p>foo   bar</p></body></html>`)
	if strings.Contains(got, "<") || strings.Contains(got, "x()") {
		t.Fatalf("tag/script harus hilang: %q", got)
	}
	if !strings.Contains(got, "Halo & Hai") || !strings.Contains(got, "foo bar") {
		t.Fatalf("teks hilang: %q", got)
	}
}

func TestParseSearchHTMLPure(t *testing.T) {
	h := `<html><body><a href="https://example.com/a">Contoh Judul A</a><p>Snippet contoh yang cukup panjang untuk lolos filter minimal dua puluh karakter.</p><a href="https://example.com/b">Judul B Kedua</a></body></html>`
	res := parseSearchHTML(h, 5)
	if len(res) != 2 {
		t.Fatalf("parse = %d, want 2: %+v", len(res), res)
	}
	if res[0].Title != "Contoh Judul A" || res[0].URL != "https://example.com/a" {
		t.Fatalf("hasil pertama salah: %+v", res[0])
	}
	if !strings.Contains(formatWebResults(res), "Contoh Judul A - https://example.com/a") {
		t.Fatalf("format salah: %q", formatWebResults(res))
	}
}

func TestWebToolsOnToolHook(t *testing.T) {
	fired := map[string]bool{}
	opts := &ProcessOptions{OnTool: func(name string, args map[string]any) { fired[name] = true }}
	tools := BuildTools(testAgent(t.TempDir()), opts)
	tools["web_search"].Run(context.Background(), map[string]any{"query": ""})
	tools["web_fetch"].Run(context.Background(), map[string]any{"url": "file:///x"})
	if !fired["web_search"] || !fired["web_fetch"] {
		t.Fatalf("hook OnTool harus jalan untuk web tools: %v", fired)
	}
}

func TestWebToolsRegistered(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	for _, n := range []string{"web_search", "web_fetch"} {
		if tools[n] == nil {
			t.Fatalf("tool %s missing", n)
		}
	}
}
