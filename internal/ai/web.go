// Web tools (stdlib only): web_search via Yahoo HTML + Bing fallback,
// web_fetch with HTML-to-text stripping. No new dependencies.
package ai

import (
	"context"
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
	webBrowserUA      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

type webResult struct {
	Title   string
	URL     string
	Snippet string
}

var (
	webAnchorRe = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]+)"[^>]*>(.*?)</a>`)
	webParaRe   = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
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
		return "", fmt.Errorf("url tidak valid: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("url harus http/https (tolak %s)", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("url harus punya host")
	}
	host := u.Hostname()
	if isPrivateHost(host) {
		return "", fmt.Errorf("host lokal/private ditolak: %s", host)
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

// unwrapResultURL decodes Yahoo redirect wrappers
// (https://r.search.yahoo.com/.../RU=<escaped>/RK=...).
func unwrapResultURL(raw string) string {
	u := html.UnescapeString(strings.ReplaceAll(raw, "&amp;", "&"))
	if strings.Contains(u, "r.search.yahoo.com") {
		if i := strings.Index(u, "/RU="); i >= 0 {
			enc := u[i+4:]
			if j := strings.Index(enc, "/RK="); j >= 0 {
				enc = enc[:j]
			}
			if dec, err := url.QueryUnescape(enc); err == nil && dec != "" {
				return dec
			}
		}
	}
	return u
}

func isEngineInternal(raw string) bool {
	l := strings.ToLower(raw)
	return strings.Contains(l, "yahoo.com") || strings.Contains(l, "bing.com") ||
		strings.Contains(l, "microsoft.com") || strings.Contains(l, "r.msn.com")
}

// parseSearchHTML extracts title+URL+snippet generically so the same
// parser works for Yahoo and Bing HTML.
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
		title := stripHTMLToText(h[m[4]:m[5]])
		if len(title) < 3 || len(title) > 300 {
			continue
		}
		// Skip nav/chrome anchors (login, settings, images...).
		lt := strings.ToLower(title)
		if lt == "login" || lt == "sign in" || lt == "settings" || lt == "images" || lt == "videos" || lt == "maps" {
			continue
		}
		snippet := ""
		end := m[1]
		if end < len(h) {
			window := h[end:]
			if len(window) > 3000 {
				window = window[:3000]
			}
			if pm := webParaRe.FindStringSubmatch(window); len(pm) == 2 {
				snippet = stripHTMLToText(pm[1])
				if len(snippet) < 20 || len(snippet) > 600 {
					if len(snippet) > 600 {
						snippet = snippet[:600]
					} else if len(snippet) < 20 {
						snippet = ""
					}
				}
			}
		}
		seen[rawURL] = true
		out = append(out, webResult{Title: title, URL: rawURL, Snippet: snippet})
	}
	return out
}

func fetchSearchHTML(ctx context.Context, fullURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webBrowserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	client := &http.Client{} // No hardcoded timeout, let context handle it
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

func runWebSearch(ctx context.Context, query string, count int) (string, error) {
	if err := validateSearchQuery(query); err != nil {
		return "", err
	}
	if count <= 0 {
		count = defaultSearchN
	}
	yahooURL := "https://search.yahoo.com/search?p=" + url.QueryEscape(strings.TrimSpace(query))
	if htmlStr, err := fetchSearchHTML(ctx, yahooURL); err == nil {
		if res := parseSearchHTML(htmlStr, count); len(res) > 0 {
			return formatWebResults(res), nil
		}
	}
	bingURL := "https://www.bing.com/search?q=" + url.QueryEscape(strings.TrimSpace(query))
	if htmlStr, err := fetchSearchHTML(ctx, bingURL); err == nil {
		if res := parseSearchHTML(htmlStr, count); len(res) > 0 {
			return formatWebResults(res), nil
		}
	}
	return "", fmt.Errorf("pencarian gagal: Yahoo dan Bing tidak mengembalikan hasil (cek koneksi/query)")
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
		return "", fmt.Errorf("halaman kosong setelah strip HTML")
	}
	return text, nil
}
