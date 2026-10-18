package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// fetchMaxBytes caps one download so a wrong URL cannot fill the disk.
const fetchMaxBytes = 512 << 20

// runFetch downloads one URL to disk (or stdout).
//
// It exists because phone shells usually cannot do HTTPS: Android's toybox
// wget has no TLS, and there is no curl on a stock device. This command uses
// the same Go network stack and resolver as the bot, so anything that works
// for the gateway works here too. Typical phone use:
//
//	puru fetch -o "$HOME/bin/tool" https://example.com/tool
func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	out := fs.String("o", "", "file to write, \"-\" for stdout (default: URL basename)")
	maxBytes := fs.Int64("max-bytes", fetchMaxBytes, "abort when the body is larger")
	timeout := fs.Int("timeout", 600, "abort after this many seconds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: puru fetch [-o FILE|-] [--max-bytes N] [--timeout SECONDS] URL")
	}
	raw := strings.TrimSpace(fs.Arg(0))
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q (http/https only)", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("url has no host")
	}
	if *maxBytes < 1 {
		return errors.New("--max-bytes must be positive")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "puru/"+version)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	if *out == "-" {
		n, err := io.Copy(os.Stdout, io.LimitReader(resp.Body, *maxBytes+1))
		if err != nil {
			return err
		}
		if n > *maxBytes {
			return fmt.Errorf("body larger than --max-bytes %d", *maxBytes)
		}
		return nil
	}

	dest := strings.TrimSpace(*out)
	if dest == "" {
		dest = fetchDefaultName(u)
	}
	if err := os.MkdirAll(filepath.Dir(mustAbs(dest)), 0o755); err != nil {
		return err
	}
	// Download to a temporary name and rename: a failed transfer never
	// truncates or destroys a file that was already there.
	tmp := dest + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(file, io.LimitReader(resp.Body, *maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if n > *maxBytes {
		os.Remove(tmp)
		return fmt.Errorf("body larger than --max-bytes %d", *maxBytes)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}

	executable := makeExecutableIfBinary(dest)
	fmt.Printf("saved %s (%d bytes)%s\n", dest, n, executable)
	return nil
}

// fetchDefaultName turns a URL into a file name: last path segment, or
// "index.html" for a directory URL.
func fetchDefaultName(u *url.URL) string {
	base := path.Base(u.Path) // URL paths always use "/", even on Windows
	if base == "." || base == "/" || base == "" {
		return "index.html"
	}
	return base
}

// mustAbs is filepath.Abs with a plain fallback, used only for MkdirAll.
func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// makeExecutableIfBinary sets the exec bit when the file looks runnable (ELF or
// a shebang line) and returns a human note for the caller. This is what makes
// `puru fetch -o bin/tool URL` usable as a one-step install.
func makeExecutableIfBinary(path string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	head := make([]byte, 4)
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	n, _ := io.ReadFull(file, head)
	file.Close()
	head = head[:n]
	if !bytes.HasPrefix(head, []byte("\x7fELF")) && !bytes.HasPrefix(head, []byte("#!")) {
		return ""
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return ""
	}
	return ", executable"
}
