package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

func testSearchConfig() config.WebSearchConfig {
	return config.WebSearchConfig{
		AIStudio: config.AIStudioSearchConfig{Active: true, Model: "gemini-2.5-flash", APIKey: "test-key"},
	}
}

func testAIStudioConfig() config.AIStudioSearchConfig {
	return testSearchConfig().AIStudio
}

func testExaConfig() config.WebSearchConfig {
	return config.WebSearchConfig{
		Exa: config.ExaSearchConfig{Active: true, APIKey: "test-key"},
	}
}

func testBothConfig() config.WebSearchConfig {
	return config.WebSearchConfig{
		AIStudio: config.AIStudioSearchConfig{Active: true, Model: "gemini-2.5-flash", APIKey: "test-key"},
		Exa:      config.ExaSearchConfig{Active: true, APIKey: "test-key"},
	}
}

func testSearchAgent(ws string) *Agent {
	a := testAgent(ws)
	a.Config.WebSearch = testSearchConfig()
	return a
}

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
	if _, err := runWebSearch(context.Background(), testSearchConfig(), "", 5); err == nil {
		t.Fatalf("query kosong harus error")
	}
	tools := BuildTools(testSearchAgent(t.TempDir()), nil)
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
	if got := clampFetchLength(0); got != 100 {
		t.Errorf("length 0 -> %d, want 100", got)
	}
	if got := clampFetchLength(999999); got != 1000 {
		t.Errorf("length huge -> %d, want 1000", got)
	}
	if got := clampFetchLength(10); got != 10 {
		t.Errorf("length 10 -> %d, want 10", got)
	}
	if got := clampFetchLength(5000); got != 1000 {
		t.Errorf("length 5000 -> %d, got %d", 1000, got)
	}
	if got := clampFetchStartLine(0); got != 1 {
		t.Errorf("start_line 0 -> %d, want 1", got)
	}
	if got := clampFetchStartLine(120); got != 120 {
		t.Errorf("start_line 120 -> %d, want 120", got)
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
	// URL-empty answer result renders without dash.
	single := []webResult{{Title: "AI Studio answer", Snippet: "Jawaban."}}
	if got := formatWebResults(single); !strings.Contains(got, "1. AI Studio answer") || !strings.Contains(got, "Jawaban.") {
		t.Fatalf("format answer-only salah: %q", got)
	}
}

func TestWebToolsOnToolHook(t *testing.T) {
	fired := map[string]bool{}
	opts := &ProcessOptions{OnTool: func(name string, args map[string]any) { fired[name] = true }}
	tools := BuildTools(testSearchAgent(t.TempDir()), opts)
	tools["web_search"].Run(context.Background(), map[string]any{"query": ""})
	tools["web_fetch"].Run(context.Background(), map[string]any{"url": "file:///x"})
	if !fired["web_search"] || !fired["web_fetch"] {
		t.Fatalf("hook OnTool harus jalan untuk web tools: %v", fired)
	}
}

func TestWebSearchDisabledByDefault(t *testing.T) {
	def := BuildTools(testAgent(t.TempDir()), nil)
	if def["web_search"] != nil {
		t.Fatalf("web_search default harus mati (tidak ada di tool list)")
	}
	if def["web_fetch"] == nil {
		t.Fatalf("web_fetch harus tetap ada")
	}
	if len(def) != 14 {
		t.Fatalf("default tools = %d, want 14 (web_search opt-in)", len(def))
	}
	en := BuildTools(testSearchAgent(t.TempDir()), nil)
	if en["web_search"] == nil {
		t.Fatalf("web_search harus ada saat aistudio active")
	}
	if len(en) != 15 {
		t.Fatalf("enabled tools = %d, want 15", len(en))
	}
	off := testAgent(t.TempDir())
	off.Config.WebSearch.AIStudio = config.AIStudioSearchConfig{Active: true, Model: "gemini-2.5-flash"}
	if BuildTools(off, nil)["web_search"] != nil {
		t.Fatalf("web_search tanpa api key harus tetap mati")
	}
	off2 := testAgent(t.TempDir())
	off2.Config.WebSearch.AIStudio = config.AIStudioSearchConfig{Active: false, Model: "m", APIKey: "k"}
	if BuildTools(off2, nil)["web_search"] != nil {
		t.Fatalf("web_search active=false harus mati")
	}
	exaOnly := testAgent(t.TempDir())
	exaOnly.Config.WebSearch = config.WebSearchConfig{Exa: config.ExaSearchConfig{Active: true, APIKey: "k"}}
	if BuildTools(exaOnly, nil)["web_search"] == nil {
		t.Fatalf("web_search harus ada saat exa active")
	}
	exaNoKey := testAgent(t.TempDir())
	exaNoKey.Config.WebSearch = config.WebSearchConfig{Exa: config.ExaSearchConfig{Active: true}}
	if BuildTools(exaNoKey, nil)["web_search"] != nil {
		t.Fatalf("web_search exa tanpa api key harus tetap mati")
	}
}

func TestAistudioSearchURL(t *testing.T) {
	u := aistudioSearchURL("gemini-2.5-flash")
	if !strings.Contains(u, "/models/gemini-2.5-flash:generateContent") {
		t.Fatalf("url salah: %q", u)
	}
	u2 := aistudioSearchURL("models/gemma-4-31b-it")
	if !strings.Contains(u2, "/models/gemma-4-31b-it:generateContent") || strings.Contains(u2, "models/models/") {
		t.Fatalf("models/ prefix harus di-strip: %q", u2)
	}
}

func aistudioTestServer(t *testing.T, body string, check func(r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("path harus :generateContent, got %s", r.URL.Path)
		}
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func TestFetchAIStudioSearchMapsResults(t *testing.T) {
	srv := aistudioTestServer(t, `{"candidates":[{"content":{"parts":[{"text":"Harga emas Rp2.595.000"}],"role":"model"},"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://example.com/a","title":"logammulia.com"}},{"web":{"uri":"https://example.com/b","title":"galeri24.co.id"}}]}}]}`, func(r *http.Request) {
		if r.URL.Query().Get("key") == "" {
			t.Errorf("key kosong sampai ke API")
		}
	})
	defer srv.Close()
	old := aistudioAPIBase
	aistudioAPIBase = srv.URL
	defer func() { aistudioAPIBase = old }()

	res, err := fetchAIStudioSearch(context.Background(), testAIStudioConfig(), "harga emas", 5)
	if err != nil {
		t.Fatalf("fetchAIStudioSearch error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("hasil = %d, want 2: %+v", len(res), res)
	}
	if res[0].Title != "logammulia.com" || res[0].URL != "https://example.com/a" {
		t.Fatalf("hasil pertama salah: %+v", res[0])
	}
	if !strings.Contains(res[0].Snippet, "Rp2.595.000") {
		t.Fatalf("jawaban grounded harus jadi snippet: %+v", res[0])
	}
	if res[1].Snippet != "" {
		t.Fatalf("chunk kedua harus tanpa snippet: %+v", res[1])
	}
}

func TestFetchAIStudioSearchAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}`))
	}))
	defer srv.Close()
	old := aistudioAPIBase
	aistudioAPIBase = srv.URL
	defer func() { aistudioAPIBase = old }()

	if _, err := fetchAIStudioSearch(context.Background(), testAIStudioConfig(), "x", 5); err == nil {
		t.Fatalf("HTTP 400 harus jadi error")
	} else if !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("pesan API harus diteruskan: %v", err)
	}
	if _, err := runWebSearch(context.Background(), testSearchConfig(), "", 5); err == nil {
		t.Fatalf("query kosong harus error")
	}
	off := config.WebSearchConfig{AIStudio: config.AIStudioSearchConfig{Active: false, Model: "m", APIKey: "k"}}
	if _, err := runWebSearch(context.Background(), off, "x", 5); err == nil {
		t.Fatalf("inactive harus error")
	}
}

func TestFetchAIStudioAnswerWithoutChunks(t *testing.T) {
	srv := aistudioTestServer(t, `{"candidates":[{"content":{"parts":[{"text":"Emas naik hari ini."}],"role":"model"},"groundingMetadata":{}}]}`, nil)
	defer srv.Close()
	old := aistudioAPIBase
	aistudioAPIBase = srv.URL
	defer func() { aistudioAPIBase = old }()

	res, err := fetchAIStudioSearch(context.Background(), testAIStudioConfig(), "emas", 5)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(res) != 1 || res[0].Title != "AI Studio answer" {
		t.Fatalf("tanpa chunk harus 1 answer: %+v", res)
	}
	if !strings.Contains(res[0].Snippet, "Emas naik") {
		t.Fatalf("snippet hilang: %+v", res[0])
	}
}

func TestRunWebSearchFormatsOutput(t *testing.T) {
	srv := aistudioTestServer(t, `{"candidates":[{"content":{"parts":[{"text":"Install Go quickly"}],"role":"model"},"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://go.dev/doc/install","title":"Go Dev"}}]}}]}`, nil)
	defer srv.Close()
	old := aistudioAPIBase
	aistudioAPIBase = srv.URL
	defer func() { aistudioAPIBase = old }()

	out, err := runWebSearch(context.Background(), testSearchConfig(), "go install", 5)
	if err != nil {
		t.Fatalf("runWebSearch error: %v", err)
	}
	if !strings.Contains(out, "Go Dev - https://go.dev/doc/install") {
		t.Fatalf("output salah: %q", out)
	}
}

func exaTestServer(t *testing.T, body string, status int, check func(r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/search") {
			t.Errorf("path harus /search, got %s", r.URL.Path)
		}
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		if status != 0 && status != 200 {
			w.WriteHeader(status)
		}
		w.Write([]byte(body))
	}))
}

func TestFetchExaSearchMapsResults(t *testing.T) {
	srv := exaTestServer(t, `{"results":[{"title":"Nvidia pushes AI security","url":"https://example.com/nvidia","highlights":["Nvidia CEO Jensen Huang says jailbreaks are an engineering problem"]},{"title":"Go Dev","url":"https://go.dev/doc/install"}]}`, 200, func(r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Errorf("x-api-key kosong sampai ke API")
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %q, want application/json", ct)
		}
	})
	defer srv.Close()
	old := exaAPIBase
	exaAPIBase = srv.URL
	defer func() { exaAPIBase = old }()

	res, err := fetchExaSearch(context.Background(), testExaConfig().Exa, "nvidia", 5)
	if err != nil {
		t.Fatalf("fetchExaSearch error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("hasil = %d, want 2: %+v", len(res), res)
	}
	if res[0].Title != "Nvidia pushes AI security" || res[0].URL != "https://example.com/nvidia" {
		t.Fatalf("hasil pertama salah: %+v", res[0])
	}
	if !strings.Contains(res[0].Snippet, "engineering problem") {
		t.Fatalf("highlights harus jadi snippet: %+v", res[0])
	}
}

func TestFetchExaSearchAPIFailure(t *testing.T) {
	srv := exaTestServer(t, `{"error":"unauthorized"}`, 401, nil)
	defer srv.Close()
	old := exaAPIBase
	exaAPIBase = srv.URL
	defer func() { exaAPIBase = old }()

	if _, err := fetchExaSearch(context.Background(), testExaConfig().Exa, "x", 5); err == nil {
		t.Fatalf("HTTP 401 harus jadi error")
	}
}

func TestRunWebSearchExaOnly(t *testing.T) {
	srv := exaTestServer(t, `{"results":[{"title":"Exa Hit","url":"https://example.com/hit","highlights":["cuplikan exa"]}]}`, 200, nil)
	defer srv.Close()
	old := exaAPIBase
	exaAPIBase = srv.URL
	defer func() { exaAPIBase = old }()

	out, err := runWebSearch(context.Background(), testExaConfig(), "exa saja", 5)
	if err != nil {
		t.Fatalf("runWebSearch exa error: %v", err)
	}
	if !strings.Contains(out, "Exa Hit - https://example.com/hit") || !strings.Contains(out, "cuplikan exa") {
		t.Fatalf("output exa salah: %q", out)
	}
}

func TestRunWebSearchFallsBackToExa(t *testing.T) {
	// aistudio fails, exa succeeds — fallback must return exa results.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":400,"message":"API key not valid","status":"INVALID_ARGUMENT"}}`))
	}))
	defer bad.Close()
	good := exaTestServer(t, `{"results":[{"title":"Fallback Hit","url":"https://example.com/fallback","highlights":["dari exa"]}]}`, 200, nil)
	defer good.Close()
	oldAI, oldExa := aistudioAPIBase, exaAPIBase
	aistudioAPIBase, exaAPIBase = bad.URL, good.URL
	defer func() { aistudioAPIBase, exaAPIBase = oldAI, oldExa }()

	out, err := runWebSearch(context.Background(), testBothConfig(), "fallback", 5)
	if err != nil {
		t.Fatalf("fallback ke exa harus sukses: %v", err)
	}
	if !strings.Contains(out, "Fallback Hit - https://example.com/fallback") {
		t.Fatalf("hasil fallback salah: %q", out)
	}
}

func TestRunWebSearchBothFailJoinsErrors(t *testing.T) {
	badAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":400,"message":"bad ai key","status":"INVALID_ARGUMENT"}}`))
	}))
	defer badAI.Close()
	badExa := exaTestServer(t, `{"error":"bad exa key"}`, 401, nil)
	defer badExa.Close()
	oldAI, oldExa := aistudioAPIBase, exaAPIBase
	aistudioAPIBase, exaAPIBase = badAI.URL, badExa.URL
	defer func() { aistudioAPIBase, exaAPIBase = oldAI, oldExa }()

	_, err := runWebSearch(context.Background(), testBothConfig(), "gagal semua", 5)
	if err == nil {
		t.Fatalf("dua-duanya gagal harus error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "aistudio:") || !strings.Contains(msg, "exa:") {
		t.Fatalf("error harus sebut dua provider: %v", err)
	}
}

func TestWebSearchSchemaHasNoLang(t *testing.T) {
	tools := BuildTools(testSearchAgent(t.TempDir()), nil)
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

	text, err := fetchDirectFetch(context.Background(), srv.URL, "text", 1, 100)
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

	text, err := runWebFetch(context.Background(), srv.URL, "html", 1, 100)
	if err != nil {
		t.Fatalf("runWebFetch html error: %v", err)
	}
	if !strings.Contains(text, "<h1>Hi</h1>") {
		t.Fatalf("html mentah hilang: %q", text)
	}
}

func TestFetchDirectPagination(t *testing.T) {
	long := strings.Repeat("lorem ipsum dolor\n", 250)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(long))
	}))
	defer srv.Close()
	old := allowPrivateFetchHost
	allowPrivateFetchHost = true
	defer func() { allowPrivateFetchHost = old }()

	text, err := runWebFetch(context.Background(), srv.URL, "text", 1, 100)
	if err != nil {
		t.Fatalf("runWebFetch error: %v", err)
	}
	if !strings.Contains(text, "call web_fetch again with section text start_line 101") {
		t.Fatalf("penanda paginasi salah: %q", text[len(text)-200:])
	}
	text2, err := runWebFetch(context.Background(), srv.URL, "text", 101, 100)
	if err != nil {
		t.Fatalf("page 2 error: %v", err)
	}
	if got := strings.Count(text2, "lorem ipsum dolor"); got != 100 {
		t.Fatalf("page 2 lines = %d, want 100", got)
	}
	if _, err := runWebFetch(context.Background(), srv.URL, "text", 99999, 100); err == nil {
		t.Fatalf("start_line lewat harus error")
	}
}

func TestFetchDirectRejects(t *testing.T) {
	if _, err := fetchDirectFetch(context.Background(), "file:///etc/passwd", "text", 1, 100); err == nil {
		t.Fatalf("file:// harus ditolak")
	}
	if _, err := runWebFetch(context.Background(), "file:///etc/passwd", "text", 1, 100); err == nil {
		t.Fatalf("host non-http harus ditolak")
	}
}

func TestWebFetchSchemaUsesSectionStartLineLength(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	wf := tools["web_fetch"]
	if wf == nil {
		t.Fatal("web_fetch missing")
	}
	props, _ := wf.Parameters["properties"].(map[string]any)
	if props == nil {
		t.Fatal("web_fetch properties nil")
	}
	if props["url"] == nil || props["section"] == nil || props["start_line"] == nil || props["length"] == nil {
		t.Fatalf("web_fetch url/section/start_line/length missing: %v", props)
	}
	if _, ok := props["max_chars"]; ok {
		t.Fatalf("web_fetch max_chars must be gone: %v", props)
	}
}
