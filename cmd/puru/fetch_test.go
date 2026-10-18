package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFetchDefaultName(t *testing.T) {
	cases := map[string]string{
		"https://example.com/tool.bin":     "tool.bin",
		"http://example.com/a/b/x":         "x",
		"https://example.com/":             "index.html",
		"https://example.com":              "index.html",
		"https://example.com/x?token=abc":  "x",
		"https://example.com/dir/tool?q=1": "tool",
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		if got := fetchDefaultName(u); got != want {
			t.Errorf("fetchDefaultName(%s) = %q, want %q", raw, got, want)
		}
	}
}

func TestRunFetchDefaultsToURLName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	if err := runFetch([]string{srv.URL + "/note.txt"}); err != nil {
		t.Fatalf("runFetch: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(body) != "hello world" {
		t.Fatalf("body = %q", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.txt.part")); !os.IsNotExist(err) {
		t.Fatalf(".part file left behind")
	}
}

func TestRunFetchMarksExecutableBinaries(t *testing.T) {
	elf := append([]byte("\x7fELF"), make([]byte, 60)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(elf)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "bin", "tool")
	if err := runFetch([]string{"-o", dest, srv.URL + "/tool"}); err != nil {
		t.Fatalf("runFetch: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("ELF download not executable: %v", info.Mode())
	}
}

func TestRunFetchKeepsExistingFileOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(dest, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runFetch([]string{"-o", dest, srv.URL + "/x"}); err == nil {
		t.Fatalf("expected failure on http 500")
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "original" {
		t.Fatalf("existing file was modified: %q", body)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf(".part file left behind")
	}
}

func TestRunFetchRejectsBadRequests(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"file:///etc/passwd"},
		{"ftp://example.com/x"},
		{"https://example.com", "extra"},
	} {
		if err := runFetch(args); err == nil {
			t.Errorf("runFetch(%v) should fail", args)
		}
	}
}

func TestRunDoctorOffline(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	path := filepath.Join(dir, "config.json")
	cfg := `{"telegram_bot_token":"123:abc","workspace":` + jsonString(workspace) +
		`,"model":{"base_url":"https://api.example.com/v1","api_key":"k","model":"m"}}`
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runDoctor([]string{"--config", path, "--offline"}); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
}

func TestRunDoctorReportsBrokenConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runDoctor([]string{"--config", path, "--offline"}); err == nil {
		t.Fatalf("expected doctor to fail on a broken config")
	}
}

func TestDoctorToolReportSplitsFoundAndMissing(t *testing.T) {
	found, missing := doctorToolReport([]string{"sh", "puru-not-a-real-binary-9000"})
	if len(found) == 0 || len(missing) != 1 {
		t.Fatalf("found=%v missing=%v", found, missing)
	}
}

func TestBaseURLHost(t *testing.T) {
	if got := baseURLHost("https://api.example.com/v1"); got != "api.example.com" {
		t.Fatalf("host = %q", got)
	}
	if got := baseURLHost("  "); got != "" {
		t.Fatalf("blank host = %q", got)
	}
}

func TestCheckWritableDirProbes(t *testing.T) {
	if err := checkWritableDir(t.TempDir()); err != nil {
		t.Fatalf("writable temp dir failed: %v", err)
	}
	if err := checkWritableDir(""); err == nil {
		t.Fatalf("empty dir must fail")
	}
}

func parseTestURL(t *testing.T, raw string) (*url.URL, error) {
	t.Helper()
	return url.Parse(raw)
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
