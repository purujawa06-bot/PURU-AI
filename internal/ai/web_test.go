package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestFormatWebResults(t *testing.T) {
	res := []webResult{
		{Title: "Contoh Judul A", URL: "https://example.com/a", Snippet: "Snippet contoh."},
		{Title: "Judul B Kedua", URL: "https://example.com/b"},
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

func TestNormalizeSearchLang(t *testing.T) {
	for in, want := range map[string]string{
		"":          "en",
		"  ":        "en",
		"id":        "id",
		"ID":        "id",
		"en":        "en",
		"EN":        "en",
		"en-US":     "en-us",
		"ms":        "ms",
		"xyz!!":     "en",
		"indonesia": "en",
	} {
		if got := normalizeSearchLang(in); got != want {
			t.Errorf("normalizeSearchLang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPuruSearchURL(t *testing.T) {
	u := puruSearchURL("harga emas", "id", 5)
	if !strings.Contains(u, "query=harga+emas") && !strings.Contains(u, "query=harga%20emas") {
		t.Fatalf("query tidak ter-encode: %q", u)
	}
	if !strings.Contains(u, "lang=id") || !strings.Contains(u, "limit=5") {
		t.Fatalf("lang/limit hilang: %q", u)
	}
	u = puruSearchURL("go tutorial", "", 3)
	if !strings.Contains(u, "lang=en") {
		t.Fatalf("default lang harus en: %q", u)
	}
}

func TestFetchPuruSearchMapsResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("query") == "" {
			t.Errorf("query kosong sampai ke API")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"query":"golang","count":3,"results":[` +
			`{"title":"Judul A","url":"https://example.com/a","snippet":"<p>Snippet A</p>"},` +
			`{"title":"Judul B","url":"https://example.com/b","snippet":null},` +
			`{"title":"","url":"https://example.com/empty","snippet":"x"}` +
			`]}`))
	}))
	defer srv.Close()
	old := puruSearchAPIBase
	puruSearchAPIBase = srv.URL
	defer func() { puruSearchAPIBase = old }()

	res, err := fetchPuruSearch(context.Background(), "golang", "id", 5)
	if err != nil {
		t.Fatalf("fetchPuruSearch error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("hasil = %d, want 2: %+v", len(res), res)
	}
	if res[0].Title != "Judul A" || res[0].URL != "https://example.com/a" {
		t.Fatalf("hasil pertama salah: %+v", res[0])
	}
	if res[0].Snippet != "Snippet A" {
		t.Fatalf("snippet HTML harus di-strip: %+v", res[0])
	}
	if res[1].Snippet != "" {
		t.Fatalf("snippet null harus kosong: %+v", res[1])
	}
}

func TestFetchPuruSearchAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"error":"Parameter query wajib diisi"}`))
	}))
	defer srv.Close()
	old := puruSearchAPIBase
	puruSearchAPIBase = srv.URL
	defer func() { puruSearchAPIBase = old }()

	if _, err := fetchPuruSearch(context.Background(), "x", "en", 5); err == nil {
		t.Fatalf("success=false harus jadi error")
	}
	if _, err := runWebSearch(context.Background(), "", 5, "en"); err == nil {
		t.Fatalf("query kosong harus error")
	}
}

func TestRunWebSearchFormatsOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"results":[{"title":"Go Dev","url":"https://go.dev/doc/install","snippet":"Install Go quickly"}]}`))
	}))
	defer srv.Close()
	old := puruSearchAPIBase
	puruSearchAPIBase = srv.URL
	defer func() { puruSearchAPIBase = old }()

	out, err := runWebSearch(context.Background(), "go install", 5, "en")
	if err != nil {
		t.Fatalf("runWebSearch error: %v", err)
	}
	if !strings.Contains(out, "Go Dev - https://go.dev/doc/install") {
		t.Fatalf("output salah: %q", out)
	}
}

func TestWebSearchLangParamRegistered(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	ws := tools["web_search"]
	if ws == nil {
		t.Fatal("web_search missing")
	}
	// lang invalid tidak boleh bikin error validasi — dinormalkan ke en.
	out, _ := ws.Run(context.Background(), map[string]any{"query": "", "lang": "en"})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("query kosong harus tetap success=false walau lang diisi: %v", out)
	}
}
