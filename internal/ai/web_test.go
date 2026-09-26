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
		t.Fatalf("empty query must be rejected")
	}
	if err := validateSearchQuery("   "); err == nil {
		t.Fatalf("whitespace query must be rejected")
	}
	if err := validateSearchQuery("golang"); err != nil {
		t.Fatalf("valid query rejected: %v", err)
	}
	tools := BuildTools(testAgent(t.TempDir()), nil)
	out, _ := tools["web_search"].Run(context.Background(), map[string]any{"query": ""})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("web_search empty query must return success=false: %v", out)
	}
}

func TestWebFetchRejectsNonHTTP(t *testing.T) {
	for _, u := range []string{"", "file:///etc/passwd", "ftp://x.com/a", "gopher://x/", "notaurl-no-scheme"} {
		if _, err := validateFetchURL(u); err == nil {
			t.Errorf("url %q must be rejected", u)
		}
	}
	for _, u := range []string{"http://localhost/x", "http://127.0.0.1/", "http://192.168.1.1/", "http://10.0.0.1/"} {
		if _, err := validateFetchURL(u); err == nil {
			t.Errorf("local host %q must be rejected", u)
		}
	}
	if _, err := validateFetchURL("https://example.com/a?b=1"); err != nil {
		t.Errorf("public url rejected: %v", err)
	}
	tools := BuildTools(testAgent(t.TempDir()), nil)
	out, _ := tools["web_fetch"].Run(context.Background(), map[string]any{"url": "file:///etc/passwd"})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("web_fetch file:// must return success=false: %v", out)
	}
}

func TestClampSearchFetch(t *testing.T) {
	if got := clampSearchLimit(0); got != 5 {
		t.Errorf("limit 0 -> %d, want 5", got)
	}
	if got := clampSearchLimit(999); got != 10 {
		t.Errorf("limit 999 -> %d, want 10", got)
	}
	if got := clampSearchLimit(-3); got != 1 {
		t.Errorf("limit -3 -> %d, want 1", got)
	}
	if got := clampSearchLimit(3); got != 3 {
		t.Errorf("limit 3 -> %d", got)
	}
	if got := clampFetchLength(0); got != 5000 {
		t.Errorf("length 0 -> %d, want 5000", got)
	}
	if got := clampFetchLength(999999); got != 20000 {
		t.Errorf("length huge -> %d, want 20000", got)
	}
	if got := clampFetchLength(0); got != defaultFetchLength {
		t.Errorf("length 0 -> %d, want default %d", got, defaultFetchLength)
	}
	if got := clampFetchLength(5000); got != 5000 {
		t.Errorf("length 5000 -> %d", got)
	}
	if got := clampFetchOffset(-5); got != 0 {
		t.Errorf("offset -5 -> %d, want 0", got)
	}
	if got := clampFetchOffset(0); got != 0 {
		t.Errorf("offset 0 -> %d", got)
	}
	if got := clampFetchOffset(5000); got != 5000 {
		t.Errorf("offset 5000 -> %d", got)
	}
}

func TestStripHTMLToText(t *testing.T) {
	got := stripHTMLToText(`<html><head><style>a{}</style></head><body><script>x()</script><h1>Halo &amp; Hai</h1><p>foo   bar</p></body></html>`)
	if strings.Contains(got, "<") || strings.Contains(got, "x()") {
		t.Fatalf("tags/script must be stripped: %q", got)
	}
	if !strings.Contains(got, "Halo & Hai") || !strings.Contains(got, "foo bar") {
		t.Fatalf("text missing: %q", got)
	}
}

func TestFormatWebResults(t *testing.T) {
	res := []webResult{
		{Title: "Sample Title A", URL: "https://example.com/a", Snippet: "Sample snippet."},
		{Title: "Title B", URL: "https://example.com/b"},
	}
	if !strings.Contains(formatWebResults(res), "Sample Title A - https://example.com/a") {
		t.Fatalf("bad format: %q", formatWebResults(res))
	}
}

func TestWebToolsOnToolHook(t *testing.T) {
	fired := map[string]bool{}
	opts := &ProcessOptions{OnTool: func(name string, args map[string]any) { fired[name] = true }}
	tools := BuildTools(testAgent(t.TempDir()), opts)
	tools["web_search"].Run(context.Background(), map[string]any{"query": ""})
	tools["web_fetch"].Run(context.Background(), map[string]any{"url": "file:///x"})
	if !fired["web_search"] || !fired["web_fetch"] {
		t.Fatalf("OnTool hook must fire for web tools: %v", fired)
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
		t.Fatalf("query not encoded: %q", u)
	}
	if !strings.Contains(u, "lang=id") || !strings.Contains(u, "limit=5") {
		t.Fatalf("lang/limit missing: %q", u)
	}
	u = puruSearchURL("go tutorial", "", 3)
	if !strings.Contains(u, "lang=en") {
		t.Fatalf("default lang must be en: %q", u)
	}
}

func TestFetchPuruSearchMapsResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("query") == "" {
			t.Errorf("empty query reached the API")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"query":"golang","count":3,"results":[` +
			`{"title":"Title A","url":"https://example.com/a","snippet":"<p>Snippet A</p>"},` +
			`{"title":"Title B","url":"https://example.com/b","snippet":null},` +
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
		t.Fatalf("results = %d, want 2: %+v", len(res), res)
	}
	if res[0].Title != "Title A" || res[0].URL != "https://example.com/a" {
		t.Fatalf("first result mismatch: %+v", res[0])
	}
	if res[0].Snippet != "Snippet A" {
		t.Fatalf("HTML snippet must be stripped: %+v", res[0])
	}
	if res[1].Snippet != "" {
		t.Fatalf("null snippet must be empty: %+v", res[1])
	}
}

func TestFetchPuruSearchAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"error":"query is required"}`))
	}))
	defer srv.Close()
	old := puruSearchAPIBase
	puruSearchAPIBase = srv.URL
	defer func() { puruSearchAPIBase = old }()

	if _, err := fetchPuruSearch(context.Background(), "x", "en", 5); err == nil {
		t.Fatalf("success=false must return an error")
	}
	if _, err := runWebSearch(context.Background(), "", 5, "en"); err == nil {
		t.Fatalf("empty query must return an error")
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
		t.Fatalf("bad output: %q", out)
	}
}

func TestWebSearchLangParamRegistered(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	ws := tools["web_search"]
	if ws == nil {
		t.Fatal("web_search missing")
	}
	// Invalid lang must not fail validation — it normalizes to en,
	// while the empty query still returns success=false.
	out, _ := ws.Run(context.Background(), map[string]any{"query": "", "lang": "en"})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("empty query must stay success=false with lang set: %v", out)
	}
}

func TestPuruWebFetchURL(t *testing.T) {
	u := puruWebFetchURL("https://example.com/a?b=1", 5000, 2000)
	if !strings.Contains(u, "offset=5000") || !strings.Contains(u, "length=2000") {
		t.Fatalf("offset/length missing: %q", u)
	}
	if !strings.Contains(u, "url=") || !strings.Contains(u, "example.com") {
		t.Fatalf("url param missing: %q", u)
	}
}

func TestFetchPuruWebFetchPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("url") == "" {
			t.Errorf("empty url reached the API")
		}
		if q.Get("offset") != "0" || q.Get("length") != "10" {
			t.Errorf("unexpected pagination params: offset=%q length=%q", q.Get("offset"), q.Get("length"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"url":"https://example.com/","final_url":"https://example.com/","content_type":"text/html","total_length":125,"offset":0,"length":10,"requested_length":10,"has_more":true,"content":"Example Do"}`))
	}))
	defer srv.Close()
	old := puruWebFetchAPIBase
	puruWebFetchAPIBase = srv.URL
	defer func() { puruWebFetchAPIBase = old }()

	res, err := fetchPuruWebFetch(context.Background(), "https://example.com/", 0, 10)
	if err != nil {
		t.Fatalf("fetchPuruWebFetch error: %v", err)
	}
	if res.Content != "Example Do" {
		t.Fatalf("content mismatch: %+v", res)
	}
	if !res.HasMore || res.TotalLength != 125 {
		t.Fatalf("pagination fields mismatch: %+v", res)
	}
}

func TestRunWebFetchHasMoreFooter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"url":"https://example.com/","total_length":125,"offset":0,"length":10,"requested_length":10,"has_more":true,"content":"Example Do"}`))
	}))
	defer srv.Close()
	old := puruWebFetchAPIBase
	puruWebFetchAPIBase = srv.URL
	defer func() { puruWebFetchAPIBase = old }()

	out, err := runWebFetch(context.Background(), "https://example.com/", 0, 10)
	if err != nil {
		t.Fatalf("runWebFetch error: %v", err)
	}
	if !strings.Contains(out, "Example Do") {
		t.Fatalf("content missing: %q", out)
	}
	if !strings.Contains(out, "has_more=true") || !strings.Contains(out, "offset=10") {
		t.Fatalf("pagination footer missing: %q", out)
	}
}

func TestRunWebFetchAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"error":"fetch failed"}`))
	}))
	defer srv.Close()
	old := puruWebFetchAPIBase
	puruWebFetchAPIBase = srv.URL
	defer func() { puruWebFetchAPIBase = old }()

	if _, err := runWebFetch(context.Background(), "https://example.com/", 0, 5000); err == nil {
		t.Fatalf("success=false must return an error")
	}
}

func TestWebFetchParamsRegistered(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	wf := tools["web_fetch"]
	if wf == nil {
		t.Fatal("web_fetch missing")
	}
	props, _ := wf.Parameters["properties"].(map[string]any)
	for _, p := range []string{"url", "offset", "length"} {
		if props[p] == nil {
			t.Fatalf("web_fetch param %s missing", p)
		}
	}
	if props["max_chars"] != nil {
		t.Fatalf("legacy max_chars must be gone: %v", props)
	}
	ws := tools["web_search"]
	props, _ = ws.Parameters["properties"].(map[string]any)
	if props["limit"] == nil {
		t.Fatalf("web_search param limit missing")
	}
	if props["count"] != nil {
		t.Fatalf("legacy count must be gone: %v", props)
	}
}
