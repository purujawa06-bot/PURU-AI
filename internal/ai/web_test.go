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

func TestParseSearchHTMLSingleQuoteAndNoSnippet(t *testing.T) {
	// href kutip tunggal + tanpa <p> snippet: hasil tetap valid (tidak kosong).
	h := `<html><body><a href='https://example.com/solo'>Judul Kutip Solo</a> teks lanjutan cukup panjang agar fallback snippet dapat terbentuk dengan baik.</body></html>`
	res := parseSearchHTML(h, 5)
	if len(res) != 1 {
		t.Fatalf("parse = %d, want 1: %+v", len(res), res)
	}
	if res[0].URL != "https://example.com/solo" {
		t.Fatalf("url salah: %+v", res[0])
	}
}

func TestUnwrapDDGAndYahoo(t *testing.T) {
	if got := unwrapResultURL("//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fx&rut=abc"); got != "https://example.com/x" {
		t.Fatalf("ddg unwrap salah: %q", got)
	}
	if got := unwrapResultURL("https://r.search.yahoo.com/_ylt=abc/RU=https%3A%2F%2Fexample.com%2Fy/RK=123"); got != "https://example.com/y" {
		t.Fatalf("yahoo unwrap salah: %q", got)
	}
	if got := unwrapResultURL("https://www.bing.com/ck/a?u=a1b2c3"); got != "" {
		t.Fatalf("bing /ck/a invalid harus dilewati, got %q", got)
	}
	// Verified live: u=a1<base64> -> URL asli.
	if got := unwrapResultURL("https://www.bing.com/ck/a?!&p=13dee03c691ffff6e229e665cccf2e4ecee70d0e0c1b01f3cd2279c4023fd911JmltdHM9MTc5MDEyMTYwMA&ptn=3&hsh=4&u=a1aHR0cHM6Ly9nby5kZXYvZG9jL2luc3RhbGw&ntb=1"); got != "https://go.dev/doc/install" {
		t.Fatalf("bing u= base64 harus ke-decode, got %q", got)
	}
}

func TestParseSearchHTMLSkipsBingTracking(t *testing.T) {
	h := `<html><body><a href="https://www.bing.com/ck/a?u=a1b2">Hasil Tracking</a><a href="https://example.com/real">Hasil Asli Yang Valid</a><p>Deskripsi hasil asli yang cukup panjang untuk lolos filter snippet minimal.</p></body></html>`
	res := parseSearchHTML(h, 5)
	if len(res) != 1 || res[0].URL != "https://example.com/real" {
		t.Fatalf("tracking invalid harus dilewati, got %+v", res)
	}
}

func TestNormalizeSearchLang(t *testing.T) {
	for in, want := range map[string]string{
		"":        "en",
		"  ":      "en",
		"id":      "id",
		"ID":      "id",
		"en":      "en",
		"EN":      "en",
		"en-US":   "en-us",
		"ms":      "ms",
		"xyz!!":   "en",
		"indonesia": "en",
	} {
		if got := normalizeSearchLang(in); got != want {
			t.Errorf("normalizeSearchLang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBingSearchURLSetlang(t *testing.T) {
	u := bingSearchURL("harga emas", "id")
	if !strings.Contains(u, "bing.com/search?q=harga+emas") && !strings.Contains(u, "bing.com/search?q=harga%20emas") {
		t.Fatalf("query tidak ter-encode: %q", u)
	}
	if !strings.Contains(u, "setlang=id") || !strings.Contains(u, "cc=ID") {
		t.Fatalf("setlang/cc id hilang: %q", u)
	}
	u = bingSearchURL("go tutorial", "en")
	if !strings.Contains(u, "setlang=en") || !strings.Contains(u, "cc=US") {
		t.Fatalf("setlang/cc en hilang: %q", u)
	}
	// lang kosong/invalid jatuh ke en.
	u = bingSearchURL("x", "")
	if !strings.Contains(u, "setlang=en") {
		t.Fatalf("default lang harus en: %q", u)
	}
	if h := acceptLanguageHeader("id"); !strings.Contains(h, "id-ID") {
		t.Fatalf("accept-language id salah: %q", h)
	}
	if h := acceptLanguageHeader("en"); !strings.Contains(h, "en-US") {
		t.Fatalf("accept-language en salah: %q", h)
	}
}

func TestWebSearchLangParamRegistered(t *testing.T) {
	tools := BuildTools(testAgent(t.TempDir()), nil)
	ws := tools["web_search"]
	if ws == nil {
		t.Fatal("web_search missing")
	}
	// lang invalid tidak boleh bikin error validasi — dinormalkan ke id.
	out, _ := ws.Run(context.Background(), map[string]any{"query": "", "lang": "en"})
	m, _ := out.(map[string]any)
	if m["success"] != false {
		t.Fatalf("query kosong harus tetap success=false walau lang diisi: %v", out)
	}
}

func TestParseSearchHTMLRealBingMarkup(t *testing.T) {
	// Markup b_algo asli dari Bing live: href /ck/a dengan u=a1<base64>,
	// judul di <h2>, snippet di <p> — harus jadi 2 hasil, bukan kosong.
	h := `<li class="b_algo"><h2><a target="_blank" href="https://www.bing.com/ck/a?!&amp;&amp;p=13dee03c691ffff6e229e665cccf2e4ecee70d0e0c1b01f3cd2279c4023fd911JmltdHM9MTc5MDEyMTYwMA&amp;ptn=3&amp;ver=2&amp;hsh=4&amp;u=a1aHR0cHM6Ly9nby5kZXYvZG9jL2luc3RhbGw&amp;ntb=1" h="ID=SERP,5139.1">Download and install - The Go Programming Language</a></h2><div class="b_caption"><p>Download and install Go quickly with the steps described here. For other content on installing, you might be interested in Managing Go installations.</p></div></li>` +
		`<li class="b_algo"><h2><a target="_blank" href="https://www.bing.com/ck/a?!&amp;&amp;p=b03ae396709efdfb63fc0607ae4b19ffcf62e60999c81b5891945220a968a190JmltdHM9MTc5MDEyMTYwMA&amp;ptn=3&amp;ver=2&amp;hsh=4&amp;u=a1aHR0cHM6Ly9naXRodWIuY29tL2dvbGFuZy9nbw&amp;ntb=1" h="ID=SERP,5170.1">golang/go: The Go programming language</a></h2><div class="b_caption"><p>The Go programming language. Contribute to golang/go development by creating an account on GitHub.</p></div></li>`
	res := parseSearchHTML(h, 5)
	if len(res) != 2 {
		t.Fatalf("parse bing real = %d, want 2: %+v", len(res), res)
	}
	if res[0].URL != "https://go.dev/doc/install" {
		t.Fatalf("hasil bing 1 salah: %+v", res[0])
	}
	if res[1].URL != "https://github.com/golang/go" {
		t.Fatalf("hasil bing 2 salah: %+v", res[1])
	}
	if res[0].Snippet == "" {
		t.Fatalf("snippet bing 1 kosong: %+v", res[0])
	}
}
