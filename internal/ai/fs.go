// Package ai: lightweight local assistant agent.
//
// Tools (8, all jailed to Workspace when
// Config.RestrictWorkspace is true) — declarations mirror picoclaw:
//   - read_file (path, offset, length), write_file (path, content, overwrite),
//     list_dir (path), edit_file (path, old_text, new_text), exec (action
//     wajib; command, sessionId, keys, data, background, pty, cwd, timeout
//     opsional — hanya run yang diimplementasikan)
//   - telegram_sendfile, telegram_getuser (need a Telegram request context)
//
// No VFS, no sandbox, no web, no fallback, no skills.
package ai

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const maxExecOutput = 20_000

// maxReadFileSize caps one read_file call (picoclaw parity: 64KB,
// anti context-overflow).
const maxReadFileSize = 64 * 1024

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
		return fmt.Errorf("old_text not found in file. Make sure it matches exactly")
	}
	if n > 1 {
		return fmt.Errorf("old_text appears %d times. Please provide more context to make it unique", n)
	}
	return os.WriteFile(abs, []byte(strings.Replace(s, oldStr, newStr, 1)), 0o644)
}

// readLocalFile reads path with byte pagination (picoclaw read_file parity).
// Returns the full LLM text: "[file: base | total: N bytes | read: bytes a-b]"
// header + "[TRUNCATED ... offset=X ...]" / "[END OF FILE ...]" trailer, or
// "[END OF FILE - no content at this offset]" when offset is past the end.
func readLocalFile(workspace string, restrict bool, p string, offset, length int64) (string, error) {
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return "", err
	}
	if offset < 0 {
		return "", fmt.Errorf("offset must be >= 0")
	}
	if length <= 0 {
		return "", fmt.Errorf("length must be > 0")
	}
	if length > maxReadFileSize {
		length = maxReadFileSize
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", p)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", fmt.Errorf("failed to seek to offset %d: %w", offset, err)
	}
	probe := make([]byte, length+1)
	n, rerr := io.ReadFull(f, probe)
	if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("failed to read file content: %w", rerr)
	}
	hasMore := int64(n) > length
	if int64(n) > length {
		n = int(length)
	}
	data := probe[:n]
	if len(data) == 0 {
		return "[END OF FILE - no content at this offset]", nil
	}
	if isBinaryData(data) {
		return "", fmt.Errorf("file appears to be binary")
	}
	readEnd := offset + int64(len(data))
	header := fmt.Sprintf("[file: %s | total: %d bytes | read: bytes %d-%d]",
		filepath.Base(p), info.Size(), offset, readEnd-1)
	if hasMore {
		header += fmt.Sprintf("\n[TRUNCATED - file has more content. Call read_file again with offset=%d to continue.]", readEnd)
	} else {
		header += "\n[END OF FILE - no further content.]"
	}
	return header + "\n\n" + string(data), nil
}

// isBinaryData sniffs binary content (NUL byte or non-text ratio).
func isBinaryData(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	return false
}

// writeLocalFile writes content, replacing any existing file (picoclaw
// write_file parity). Without overwrite=true an existing file is refused with
// a hint to use append_file/edit_file instead.
func writeLocalFile(workspace string, restrict bool, p, content string, overwrite bool) error {
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Stat(abs); err == nil {
			return fmt.Errorf("file: %s already exists. To add to it or change part of it without losing the current contents, use append_file or edit_file. Only set overwrite=true if you intend to replace the entire file.", p)
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o644)
}

// appendLocalFile appends content to the end of a file, creating it when
// absent (picoclaw append_file parity).
func appendLocalFile(workspace string, restrict bool, p, content string) error {
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// listLocalDir lists files/dirs (picoclaw list_dir parity): "DIR: x" /
// "FILE: y" lines. Empty path = workspace root.
func listLocalDir(workspace string, restrict bool, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		p = "."
	}
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			sb.WriteString("DIR:  " + e.Name() + "\n")
		} else {
			sb.WriteString("FILE: " + e.Name() + "\n")
		}
	}
	if sb.Len() == 0 {
		return "(empty directory)", nil
	}
	return sb.String(), nil
}

func defaultShell() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C"}
	}
	return "sh", []string{"-c"}
}
