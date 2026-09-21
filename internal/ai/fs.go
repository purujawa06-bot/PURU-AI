// Package ai: lightweight local assistant agent.
//
// Tools (4, all jailed to Workspace when
// Config.RestrictWorkspace is true):
//   - edit_file, exec
//   - telegram_sendfile, telegram_getuser (need a Telegram request context)
//
// No VFS, no sandbox, no web, no fallback, no skills.
package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const maxExecOutput = 20_000

// resolvePath maps a tool path to an absolute filesystem path.
// When restrict is true the result is jailed inside workspace:
// absolute paths and ../ escapes are rejected.
func resolvePath(workspace string, restrict bool, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path kosong")
	}
	if restrict {
		if filepath.IsAbs(p) {
			// Allow absolute paths only when already inside workspace.
			abs := filepath.Clean(p)
			if !insideDir(abs, workspace) {
				return "", fmt.Errorf("di luar workspace: %q", p)
			}
			return abs, nil
		}
		joined := filepath.Join(workspace, filepath.FromSlash(p))
		abs, err := filepath.Abs(joined)
		if err != nil {
			return "", err
		}
		if !insideDir(abs, workspace) {
			return "", fmt.Errorf("di luar workspace: %q", p)
		}
		return abs, nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	abs, err := filepath.Abs(filepath.Join(workspace, filepath.FromSlash(p)))
	if err != nil {
		return "", err
	}
	return abs, nil
}

func insideDir(abs, dir string) bool {
	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

// resolveWorkdir jails exec workdir the same way. Empty = workspace.
func resolveWorkdir(workspace string, restrict bool, w string) (string, error) {
	if strings.TrimSpace(w) == "" {
		return workspace, nil
	}
	return resolvePath(workspace, restrict, w)
}

func editLocalFile(workspace string, restrict bool, p, oldStr, newStr string) error {
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	s := string(b)
	n := strings.Count(s, oldStr)
	if oldStr == "" || n == 0 {
		return fmt.Errorf("old_string tidak ditemukan di %s", p)
	}
	if n > 1 {
		return fmt.Errorf("old_string muncul %d kali di %s — harus unik", n, p)
	}
	return os.WriteFile(abs, []byte(strings.Replace(s, oldStr, newStr, 1)), 0o644)
}

func defaultShell() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C"}
	}
	return "sh", []string{"-c"}
}
