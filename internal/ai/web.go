// Web tools (stdlib only): web_search via PuruBoy Search API
// (https://puruboy-api.vercel.app/api/search/web), web_fetch with
// HTML-to-text stripping. No new dependencies.
package ai

import (
	"context"
	"encoding/json"
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

// puruSearchAPIBase is a var (not const) so tests can point it at an httptest server.
var puruSearchAPIBase = "https://puruboy-api.vercel.app/api/search/web"

type webResult struct {
	Title   string
	URL     string
	Snippet string
}

type puruSearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet *string `json:"snippet"`
}

type puruSearchResponse struct {
	Success bool               `json:"success"`
	Error   string             `json:"error"`
	Results []puruSearchResult `json:"results"`
}

var (
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

// normalizeSearchLang merapikan kode bahasa: huruf kecil,
// hanya [a-z] dan strip opsional (mis. "EN" -> "en", "" -> "en").
// Kode tak valid jatuh ke default "en".
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

// puruSearchURL builds the PuruBoy Search API URL:
// GET {base}?query=...&lang=...&limit=...
func puruSearchURL(query, lang string, limit int) string {
	v := url.Values{}
	v.Set("query", strings.TrimSpace(query))
	v.Set("lang", normalizeSearchLang(lang))
	v.Set("limit", fmt.Sprintf("%d", limit))
	return strings.TrimRight(puruSearchAPIBase, "/") + "?" + v.Encode()
}

// fetchPuruSearch calls the PuruBoy Search API and maps results to webResult.
// Snippet may be null or contain HTML — it is stripped to plain text.
func fetchPuruSearch(ctx context.Context, query, lang string, limit int) ([]webResult, error) {
	ectx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ectx, "GET", puruSearchURL(query, lang, limit), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", webBrowserUA)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{} // Timeout via context di atas
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return nil, fmt.Errorf("search API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var sr puruSearchResponse
	if err := json.Unmarshal(b, &sr); err != nil {
		return nil, fmt.Errorf("search API bad JSON: %v", err)
	}
	if !sr.Success {
		msg := strings.TrimSpace(sr.Error)
		if msg == "" {
			msg = "search API returned success=false"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	seen := map[string]bool{}
	var out []webResult
	for _, r := range sr.Results {
		if len(out) >= limit {
			break
		}
		title := strings.TrimSpace(r.Title)
		u := strings.TrimSpace(r.URL)
		if title == "" || u == "" || seen[u] {
			continue
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		seen[u] = true
		snip := ""
		if r.Snippet != nil {
			snip = stripHTMLToText(*r.Snippet)
			if len(snip) > 600 {
				snip = strings.TrimSpace(snip[:600])
			}
		}
		out = append(out, webResult{Title: title, URL: u, Snippet: snip})
	}
	return out, nil
}

// runWebSearch searches via the PuruBoy Search API only.
// lang is customizable per tool call, e.g. "id" for Indonesian results.
func runWebSearch(ctx context.Context, query string, count int, lang string) (string, error) {
	if err := validateSearchQuery(query); err != nil {
		return "", err
	}
	if count <= 0 {
		count = defaultSearchN
	}
	if count > 10 {
		count = 10
	}
	lang = normalizeSearchLang(lang)
	res, err := fetchPuruSearch(ctx, query, lang, count)
	if err != nil {
		return "", fmt.Errorf("web search failed: %v", err)
	}
	if len(res) == 0 {
		return "", fmt.Errorf("search API returned no results (check connection/query)")
	}
	return formatWebResults(res), nil
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
