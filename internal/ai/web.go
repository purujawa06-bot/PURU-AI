// Web tools (stdlib only): web_search via Bing HTML saja (setlang
// Indonesia default, lang bisa di-custom per tool call), web_fetch with
// HTML-to-text stripping. No new dependencies.
package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	webSearchTimeout  = 60 * time.Second
	webFetchTimeout   = 120 * time.Second
	webFetchMaxBody   = 100 * 1024
	defaultSearchN    = 5
	defaultFetchChars = 8000
	// Default search result language: English (override per call via lang).
	defaultSearchLang = "en"
	webBrowserUA      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

type webResult struct {
	Title   string
	URL     string
	Snippet string
}

var (
	// Terima href dengan kutip tunggal/ganda agar markup Yahoo/Bing/DDG
	// yang baru tetap kepancing (sebelumnya hanya href="http..." ganda).
	webAnchorRe = regexp.MustCompile(`(?is)<a[^>]+href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	webParaRe   = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	webSpanRe   = regexp.MustCompile(`(?is)<span[^>]*>(.*?)</span>`)
	webDivRe    = regexp.MustCompile(`(?is)<div[^>]*>(.*?)</div>`)
	webScriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>`)
	webTagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	webSpaceRe  = regexp.MustCompile(`\s+`)
)

// clampSearchCount defaults to 5 when unset (0), clamps 1-10.
func clampSearchCount(n int64) int {
	if n == 0 {
		return defaultSearchN
	}
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return int(n)
}

// normalizeSearchLang merapikan kode bahasa untuk Bing setlang: huruf kecil,
// hanya [a-z] dan strip opsional (mis. "EN" -> "en", "" -> "id").
// Kode tak valid jatuh ke default "id".
func normalizeSearchLang(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	if l == "" {
		return defaultSearchLang
	}
	l = strings.ReplaceAll(l, "_", "-")
	if len(l) == 2 && isASCIILetters(l) {
		return l
	}
	if len(l) == 5 && l[2] == '-' && isASCIILetters(l[:2]) && isASCIILetters(l[3:]) {
		return l
	}
	return defaultSearchLang
}

func isASCIILetters(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return true
}

// bingCountry memetakan lang ke param cc Bing (negara hasil).
func bingCountry(lang string) string {
	switch strings.ToLower(lang) {
	case "id":
		return "ID"
	case "en", "en-us":
		return "US"
	case "en-gb":
		return "GB"
	case "ms", "ms-my":
		return "MY"
	}
	if len(lang) == 5 && lang[2] == '-' {
		return strings.ToUpper(lang[3:])
	}
	return strings.ToUpper(lang)
}

// bingSearchURL membangun URL pencarian Bing dengan setlang+cc sesuai lang.
func bingSearchURL(query, lang string) string {
	lang = normalizeSearchLang(lang)
	return "https://www.bing.com/search?q=" + url.QueryEscape(strings.TrimSpace(query)) +
		"&setlang=" + lang + "&cc=" + bingCountry(lang)
}

// acceptLanguageHeader menyelaraskan header Accept-Language dengan lang
// agar snippet/deskripsi Bing datang dalam bahasa yang diminta.
func acceptLanguageHeader(lang string) string {
	switch normalizeSearchLang(lang) {
	case "id":
		return "id-ID,id;q=0.9,en-US;q=0.7,en;q=0.5"
	case "en", "en-us":
		return "en-US,en;q=0.9"
	default:
		l := normalizeSearchLang(lang)
		return l + ";q=0.9,id;q=0.5,en;q=0.4"
	}
}

// clampFetchChars defaults to 8000 when unset (0), clamps 1000-20000.
func clampFetchChars(n int64) int {
	if n == 0 {
		return defaultFetchChars
	}
	if n < 1000 {
		return 1000
	}
	if n > 20000 {
		return 20000
	}
	return int(n)
}

func validateSearchQuery(q string) error {
	if strings.TrimSpace(q) == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

// validateFetchURL rejects non-http(s), empty host, and local/private hosts.
func validateFetchURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("url must be http/https (rejected %s)", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("url must have a host")
	}
	host := u.Hostname()
	if isPrivateHost(host) {
		return "", fmt.Errorf("local/private host rejected: %s", host)
	}
	return s, nil
}

func isPrivateHost(host string) bool {
	h := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if h == "" || h == "localhost" || h == "localhost.localdomain" ||
		h == "0.0.0.0" || h == "::1" || h == "::" {
		return true
	}
	if strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".localhost") ||
		strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".lan") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	if strings.HasPrefix(h, "127.") || strings.HasPrefix(h, "10.") || strings.HasPrefix(h, "192.168.") {
		return true
	}
	if strings.HasPrefix(h, "172.") {
		rest := strings.TrimPrefix(h, "172.")
		n := 0
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		if n >= 16 && n <= 31 {
			return true
		}
	}
	return false
}

// stripHTMLToText removes script/style, tags, unescapes entities,
// and collapses whitespace to single spaces.
func stripHTMLToText(s string) string {
	s = webScriptRe.ReplaceAllString(s, " ")
	s = webTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return webSpaceRe.ReplaceAllString(strings.TrimSpace(s), " ")
}

// unwrapResultURL decodes engine redirect wrappers ke URL target asli:
// Yahoo (r.search.yahoo.com/.../RU=<escaped>/RK=...), DuckDuckGo
// (/l/?uddg=<escaped>), Google (/url?q=...), dan param generik ?u=/?url=.
// Bing /ck/a memakai u=a1<base64-target> — bisa di-decode (verified live).
func unwrapResultURL(raw string) string {
	u := html.UnescapeString(strings.ReplaceAll(raw, "&amp;", "&"))
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	lu := strings.ToLower(u)
	if strings.Contains(lu, "r.search.yahoo.com") {
		if i := strings.Index(u, "/RU="); i >= 0 {
			enc := u[i+4:]
			if j := strings.Index(enc, "/RK="); j >= 0 {
				enc = enc[:j]
			}
			if dec, err := url.QueryUnescape(enc); err == nil && dec != "" {
				return strings.TrimSpace(dec)
			}
		}
		return ""
	}
	// Wrapper relatif / absolut dengan target di query (ddg uddg, google
	// url?q, generik u=/url=). Ambil param pertama yang terlihat seperti URL.
	if strings.HasPrefix(u, "/") || strings.Contains(lu, "duckduckgo.com/l/") ||
		strings.Contains(lu, "/url?") || strings.Contains(lu, "uddg=") {
		if parsed, err := url.Parse(u); err == nil {
			q := parsed.Query()
			for _, key := range []string{"uddg", "q", "url", "u"} {
				if v := strings.TrimSpace(q.Get(key)); v != "" &&
					(strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")) {
					return v
				}
			}
		}
		if strings.HasPrefix(u, "/") {
			return ""
		}
	}
	// Bing click-tracking: u=a1<base64-target> (tanpa padding).
	// Contoh live: u=a1aHR0cHM6Ly9nby5kZXYvZG9jL2luc3RhbGw
	// -> https://go.dev/doc/install. Tanpa ini Bing selalu kosong.
	if strings.Contains(lu, "bing.com/ck/a") || strings.Contains(lu, "bing.com/aclk") {
		if dec := decodeBingU(u); dec != "" {
			return dec
		}
		return ""
	}
	return u
}

// decodeBingU mengekstrak param u= mentah (tanpa Query().Get agar base64
// yang mengandung '+' tidak rusak jadi spasi) lalu base64-decode.
func decodeBingU(u string) string {
	raw := ""
	if i := strings.Index(u, "?"); i >= 0 {
		for _, kv := range strings.Split(u[i+1:], "&") {
			if v, ok := strings.CutPrefix(kv, "u="); ok {
				raw = v
				break
			}
			if v, ok := strings.CutPrefix(kv, "amp;u="); ok {
				raw = v
				break
			}
		}
	}
	if raw == "" {
		return ""
	}
	// '+' mentah adalah base64, bukan spasi: lindungi sebelum unescape.
	dec, err := url.QueryUnescape(strings.ReplaceAll(raw, "+", "%2B"))
	if err != nil || dec == "" {
		return ""
	}
	s := strings.TrimSpace(dec)
	s = strings.TrimPrefix(s, "a1")
	if s == "" {
		return ""
	}
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if t := strings.TrimSpace(string(b)); strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
				return t
			}
		}
	}
	// Coba dengan padding ditambah (StdEncoding).
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if t := strings.TrimSpace(string(b)); strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
				return t
			}
		}
	}
	return ""
}

func isEngineInternal(raw string) bool {
	l := strings.ToLower(raw)
	return strings.Contains(l, "yahoo.com") || strings.Contains(l, "bing.com") ||
		strings.Contains(l, "microsoft.com") || strings.Contains(l, "r.msn.com") ||
		strings.Contains(l, "duckduckgo.com") || strings.Contains(l, "google.com") ||
		strings.Contains(l, "gstatic.com") || strings.Contains(l, "yandex.")
}

// parseSearchHTML extracts title+URL+snippet generically so the same
// parser works for Yahoo, Bing, and DuckDuckGo HTML. Snippet opsional:
// hasil tanpa snippet tetap valid agar tidak respon kosong.
func parseSearchHTML(h string, max int) []webResult {
	if max <= 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []webResult
	matches := webAnchorRe.FindAllStringSubmatchIndex(h, -1)
	for _, m := range matches {
		if len(out) >= max {
			break
		}
		if len(m) < 6 {
			continue
		}
		rawURL := unwrapResultURL(strings.TrimSpace(h[m[2]:m[3]]))
		if rawURL == "" || seen[rawURL] || isEngineInternal(rawURL) {
			continue
		}
		if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
			continue
		}
		title := stripHTMLToText(h[m[4]:m[5]])
		if len(title) < 3 || len(title) > 300 {
			continue
		}
		// Skip nav/chrome anchors.
		lt := strings.ToLower(strings.TrimSpace(title))
		switch lt {
		case "login", "sign in", "settings", "images", "videos", "maps", "news",
			"shopping", "more", "feedback", "privacy", "terms", "help", "account",
			"search", "advanced search", "preferences":
			continue
		}
		seen[rawURL] = true
		out = append(out, webResult{Title: title, URL: rawURL, Snippet: snippetAfter(h, m[1])})
	}
	return out
}

// snippetAfter mencari cuplikan di ~3000 char setelah anchor: <p> dulu,
// lalu <span>/<div>, terakhir fallback teks polos window. Kosong bila tidak
// ada yang layak — hasil tetap dipertahankan.
func snippetAfter(h string, end int) string {
	if end < 0 || end >= len(h) {
		return ""
	}
	window := h[end:]
	if len(window) > 3000 {
		window = window[:3000]
	}
	if pm := webParaRe.FindStringSubmatch(window); len(pm) == 2 {
		if s := normSnippet(stripHTMLToText(pm[1])); s != "" {
			return s
		}
	}
	for _, re := range []*regexp.Regexp{webSpanRe, webDivRe} {
		if sm := re.FindAllStringSubmatch(window, 3); len(sm) > 0 {
			for _, g := range sm {
				if len(g) == 2 {
					if s := normSnippet(stripHTMLToText(g[1])); s != "" {
						return s
					}
				}
			}
		}
	}
	// Fallback: teks polos window (tanpa tag) dipotong 200 char.
	if t := stripHTMLToText(window); len(t) >= 30 {
		t = strings.TrimSpace(t)
		if len(t) > 200 {
			if i := strings.LastIndex(t[:200], " "); i > 80 {
				t = t[:i]
			} else {
				t = t[:200]
			}
		}
		return normSnippet(t)
	}
	return ""
}

func normSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return ""
	}
	if len(s) > 600 {
		s = s[:600]
	}
	return s
}

func fetchSearchHTML(ctx context.Context, fullURL, lang string) (string, error) {
	ectx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ectx, "GET", fullURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webBrowserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", acceptLanguageHeader(lang))
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	client := &http.Client{} // Timeout via context di atas
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// runWebSearch searches via Bing only (Yahoo is dead on bot-check 307,
// DDG is unstable) with setlang+cc per lang (default "en").
// lang is customizable per tool call, e.g. "id" for Indonesian results.
func runWebSearch(ctx context.Context, query string, count int, lang string) (string, error) {
	if err := validateSearchQuery(query); err != nil {
		return "", err
	}
	if count <= 0 {
		count = defaultSearchN
	}
	lang = normalizeSearchLang(lang)
	htmlStr, err := fetchSearchHTML(ctx, bingSearchURL(query, lang), lang)
	if err != nil {
		return "", fmt.Errorf("Bing search failed: %v", err)
	}
	if res := parseSearchHTML(htmlStr, count); len(res) > 0 {
		return formatWebResults(res), nil
	}
	return "", fmt.Errorf("Bing returned no results (check connection/query)")
}

func formatWebResults(res []webResult) string {
	var sb strings.Builder
	for i, r := range res {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "%d. %s - %s", i+1, r.Title, r.URL)
		if strings.TrimSpace(r.Snippet) != "" {
			sb.WriteString("\n" + r.Snippet)
		}
	}
	return sb.String()
}

// fetchURLText GETs url, caps body at ~100KB, strips HTML, truncates to maxChars.
func fetchURLText(ctx context.Context, rawURL string, maxChars int) (string, error) {
	clean, err := validateFetchURL(rawURL)
	if err != nil {
		return "", err
	}
	if maxChars <= 0 {
		maxChars = defaultFetchChars
	}
	req, err := http.NewRequestWithContext(ctx, "GET", clean, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webBrowserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*")
	client := &http.Client{} // No hardcoded timeout, let context handle it
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBody))
	if err != nil {
		return "", err
	}
	text := stripHTMLToText(string(b))
	total := len(text)
	if total > maxChars {
		text = text[:maxChars] + fmt.Sprintf(" ... [truncated, total %d chars]", total)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("page is empty after stripping HTML")
	}
	return text, nil
}
