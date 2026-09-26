// Web tools via PuruBoy API (stdlib only, no new dependencies):
//   web_search via https://puruboy-api.vercel.app/api/search/web
//   web_fetch via https://puruboy-api.vercel.app/api/agent-tools/web-fetch
// Tool parameters mirror the API query parameters exactly:
//   search: query, lang, limit
//   fetch: url, offset, length
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
	defaultSearchLimit = 5
	defaultFetchLength = 5000
	// Default search result language: English (override per call via lang).
	defaultSearchLang = "en"
	webBrowserUA      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

// API bases are vars (not consts) so tests can point them at httptest servers.
var (
	puruSearchAPIBase   = "https://puruboy-api.vercel.app/api/search/web"
	puruWebFetchAPIBase = "https://puruboy-api.vercel.app/api/agent-tools/web-fetch"
)

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

// puruWebFetchResponse mirrors the web-fetch API JSON payload.
type puruWebFetchResponse struct {
	Success         bool   `json:"success"`
	Error           string `json:"error"`
	URL             string `json:"url"`
	FinalURL        string `json:"final_url"`
	ContentType     string `json:"content_type"`
	TotalLength     int    `json:"total_length"`
	Offset          int    `json:"offset"`
	Length          int    `json:"length"`
	RequestedLength int    `json:"requested_length"`
	HasMore         bool   `json:"has_more"`
	Content         string `json:"content"`
}

var (
	webScriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>`)
	webTagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	webSpaceRe  = regexp.MustCompile(`\s+`)
)

// clampSearchLimit normalizes the search limit parameter (defaults to 5, clamps 1-10).
func clampSearchLimit(n int64) int {
	if n == 0 {
		return defaultSearchLimit
	}
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return int(n)
}

// normalizeSearchLang normalizes a language code, falling back to "en".
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

// clampFetchOffset ensures the fetch offset is never negative.
func clampFetchOffset(n int64) int {
	if n < 0 {
		return 0
	}
	return int(n)
}

// clampFetchLength normalizes the fetch length parameter (defaults to 5000, clamps 1-20000).
func clampFetchLength(n int64) int {
	if n == 0 {
		return defaultFetchLength
	}
	if n < 1 {
		return 1
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

// stripHTMLToText removes script/style, strips tags, unescapes entities,
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

// puruWebFetchURL builds the PuruBoy web-fetch API URL:
// GET {base}?url=...&offset=...&length=...
func puruWebFetchURL(rawURL string, offset, length int) string {
	v := url.Values{}
	v.Set("url", strings.TrimSpace(rawURL))
	v.Set("offset", fmt.Sprintf("%d", offset))
	v.Set("length", fmt.Sprintf("%d", length))
	return strings.TrimRight(puruWebFetchAPIBase, "/") + "?" + v.Encode()
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
	client := &http.Client{}
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
func runWebSearch(ctx context.Context, query string, limit int, lang string) (string, error) {
	if err := validateSearchQuery(query); err != nil {
		return "", err
	}
	limit = clampSearchLimit(int64(limit))
	lang = normalizeSearchLang(lang)
	res, err := fetchPuruSearch(ctx, query, lang, limit)
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

// fetchPuruWebFetch calls the PuruBoy web-fetch API for one page chunk.
func fetchPuruWebFetch(ctx context.Context, rawURL string, offset, length int) (*puruWebFetchResponse, error) {
	ectx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ectx, "GET", puruWebFetchURL(rawURL, offset, length), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", webBrowserUA)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return nil, fmt.Errorf("web-fetch API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var fr puruWebFetchResponse
	if err := json.Unmarshal(b, &fr); err != nil {
		return nil, fmt.Errorf("web-fetch API bad JSON: %v", err)
	}
	if !fr.Success {
		msg := strings.TrimSpace(fr.Error)
		if msg == "" {
			msg = "web-fetch API returned success=false"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return &fr, nil
}

// runWebFetch fetches a public URL as clean paginated text via the PuruBoy web-fetch API.
func runWebFetch(ctx context.Context, rawURL string, offset, length int) (string, error) {
	clean, err := validateFetchURL(rawURL)
	if err != nil {
		return "", err
	}
	offset = clampFetchOffset(int64(offset))
	length = clampFetchLength(int64(length))
	res, err := fetchPuruWebFetch(ctx, clean, offset, length)
	if err != nil {
		return "", fmt.Errorf("web fetch failed: %v", err)
	}
	content := strings.TrimSpace(res.Content)
	if content == "" {
		return "", fmt.Errorf("page is empty")
	}
	if res.HasMore {
		next := res.Offset + res.Length
		if next < 0 {
			next = offset + length
		}
		return fmt.Sprintf("%s\n\n[offset %d length %d total %d has_more=true — fetch next with offset=%d length=%d]",
			content, res.Offset, res.Length, res.TotalLength, next, length), nil
	}
	return content, nil
}
