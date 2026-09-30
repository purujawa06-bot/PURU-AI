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
	if got := clampFetchLength(0); got != 5000 {
		t.Errorf("length 0 -> %d, want 5000", got)
	}
	if got := clampFetchLength(999999); got != 20000 {
		t.Errorf("length huge -> %d, want 20000", got)
	}
	if got := clampFetchLength(10); got != 1000 {
		t.Errorf("length 10 -> %d, want 1000", got)
	}
	if got := clampFetchLength(5000); got != 5000 {
		t.Errorf("length 5000 -> %d, got %d", 5000, got)
	}
	if got := clampFetchOffset(-3); got != 0 {
		t.Errorf("offset -3 -> %d, want 0", got)
	}
	if got := clampFetchOffset(120); got != 120 {
		t.Errorf("offset 120 -> %d, want 120", got)
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

func TestPuruSearchURL(t *testing.T) {
	u := puruSearchURL("harga emas", 5)
	if !strings.Contains(u, "query=harga+emas") && !strings.Contains(u, "query=harga%20emas") {
		t.Fatalf("query tidak ter-encode: %q", u)
	}
	if !strings.Contains(u, "limit=5") {
		t.Fatalf("limit hilang: %q", u)
	}
	if strings.Contains(u, "lang=") {
		t.Fatalf("lang harus hilang dari API baru: %q", u)
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

	res, err := fetchPuruSearch(context.Background(), "golang", 5)
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

	if _, err := fetchPuruSearch(context.Background(), "x", 5); err == nil {
		t.Fatalf("success=false harus jadi error")
	}
	if _, err := runWebSearch(context.Background(), "", 5); err == nil {
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

	out, err := runWebSearch(context.Background(), "go install", 5)
	if err != nil {
		t.Fatalf("runWebSearch error: %v", err)
	}
	if !strings.Contains(out, "Go Dev - https://go.dev/doc/install") {
		t.Fatalf("output salah: %q", out)
	}
}

func TestWebSearchSchemaHasNoLang(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	ws := tools["web_search"]
	if ws == nil {
		t.Fatal("web_search missing")
	}
	props, _ := ws.Parameters["properties"].(map[string]any)
	if props == nil {
		t.Fatal("web_search properties nil")
	}
	if _, ok := props["lang"]; ok {
		t.Fatalf("web_search lang must be gone (API baru tanpa lang): %v", props)
	}
	if props["query"] == nil || props["count"] == nil {
		t.Fatalf("web_search query/count missing: %v", props)
	}
	// Legacy lang arg is ignored — empty query still fails on query validation.
	out, _ := ws.Run(context.Background(), map[string]any{"query": "", "lang": "en"})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("query kosong harus tetap success=false walau lang diisi: %v", out)
	}
}

func TestNormalizeFetchSection(t *testing.T) {
	if normalizeFetchSection("") != "text" {
		t.Errorf("empty -> text")
	}
	if normalizeFetchSection("HTML") != "html" {
		t.Errorf("HTML -> html")
	}
	if normalizeFetchSection("text") != "text" {
		t.Errorf("text -> text")
	}
	if normalizeFetchSection("bogus") != "text" {
		t.Errorf("bogus -> text")
	}
}

func TestFetchDirectText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><style>a{}</style></head><body><h1>Halo &amp; Hai</h1><p>foo bar Example Domain</p></body></html>`))
	}))
	defer srv.Close()
	old := allowPrivateFetchHost
	allowPrivateFetchHost = true
	defer func() { allowPrivateFetchHost = old }()

	text, err := fetchDirectFetch(context.Background(), srv.URL, "text", 0, 5000)
	if err != nil {
		t.Fatalf("fetchDirectFetch error: %v", err)
	}
	if !strings.Contains(text, "Halo & Hai") || !strings.Contains(text, "foo bar") {
		t.Fatalf("content salah: %q", text)
	}
	if strings.Contains(text, "<h1>") || strings.Contains(text, "call web_fetch again") {
		t.Fatalf("html/truncate marker tidak boleh ada: %q", text)
	}
}

func TestFetchDirectHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Hi</h1></body></html>`))
	}))
	defer srv.Close()
	old := allowPrivateFetchHost
	allowPrivateFetchHost = true
	defer func() { allowPrivateFetchHost = old }()

	text, err := runWebFetch(context.Background(), srv.URL, "html", 0, 5000)
	if err != nil {
		t.Fatalf("runWebFetch html error: %v", err)
	}
	if !strings.Contains(text, "<h1>Hi</h1>") {
		t.Fatalf("html mentah hilang: %q", text)
	}
}

func TestFetchDirectPagination(t *testing.T) {
	long := strings.Repeat("a", 8000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(long))
	}))
	defer srv.Close()
	old := allowPrivateFetchHost
	allowPrivateFetchHost = true
	defer func() { allowPrivateFetchHost = old }()

	text, err := runWebFetch(context.Background(), srv.URL, "text", 0, 5000)
	if err != nil {
		t.Fatalf("runWebFetch error: %v", err)
	}
	if !strings.Contains(text, "call web_fetch again with section text offset 5000") {
		t.Fatalf("penanda paginasi salah: %q", text[len(text)-200:])
	}
	text2, err := runWebFetch(context.Background(), srv.URL, "text", 5000, 5000)
	if err != nil {
		t.Fatalf("page 2 error: %v", err)
	}
	if len([]rune(text2)) != 3000 {
		t.Fatalf("page 2 len = %d, want 3000", len([]rune(text2)))
	}
	if _, err := runWebFetch(context.Background(), srv.URL, "text", 99999, 5000); err == nil {
		t.Fatalf("offset lewat harus error")
	}
}

func TestFetchDirectRejects(t *testing.T) {
	if _, err := fetchDirectFetch(context.Background(), "file:///etc/passwd", "text", 0, 5000); err == nil {
		t.Fatalf("file:// harus ditolak")
	}
	if _, err := runWebFetch(context.Background(), "file:///etc/passwd", "text", 0, 5000); err == nil {
		t.Fatalf("host non-http harus ditolak")
	}
}

func TestWebFetchSchemaUsesSectionOffsetLength(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	wf := tools["web_fetch"]
	if wf == nil {
		t.Fatal("web_fetch missing")
	}
	props, _ := wf.Parameters["properties"].(map[string]any)
	if props == nil {
		t.Fatal("web_fetch properties nil")
	}
	if props["url"] == nil || props["section"] == nil || props["offset"] == nil || props["length"] == nil {
		t.Fatalf("web_fetch url/section/offset/length missing: %v", props)
	}
	if _, ok := props["max_chars"]; ok {
		t.Fatalf("web_fetch max_chars must be gone: %v", props)
	}
}
