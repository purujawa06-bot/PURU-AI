// Package ai: lightweight local assistant agent.
//
// Tools (9, all jailed to Workspace when
// Config.RestrictWorkspace is true) — declarations mirror picoclaw:
//   - read_file (path, offset, length), write_file (path, content, overwrite),
//     list_dir (path), edit_file (path, old_text, new_text), exec (action
//     wajib: run/list/poll/read/kill; command, sessionId, background, cwd,
//     timeout opsional)
//   - telegram_sendfile, telegram_getuser (need a Telegram request context)
//   - get_env (environment info)
//
// No VFS, no sandbox, no web, no fallback, no skills.
package ai

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
// Hasil selalu dipastikan ada dan berupa direktori agar cmd.Start tak gagal
// dengan pesan chdir generik — AI dapat pesan jelas untuk koreksi mandiri.
func resolveWorkdir(workspace string, restrict bool, w string) (string, error) {
	if strings.TrimSpace(w) == "" {
		if strings.TrimSpace(workspace) == "" {
			return workspace, nil
		}
		if st, err := os.Stat(workspace); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("workspace tidak ditemukan: %s", workspace)
			}
			return "", fmt.Errorf("workspace tidak bisa diakses: %s: %v", workspace, err)
		} else if !st.IsDir() {
			return "", fmt.Errorf("workspace bukan direktori: %s", workspace)
		}
		return workspace, nil
	}
	abs, err := resolvePath(workspace, restrict, w)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("cwd tidak ditemukan: %s. Gunakan list_dir untuk melihat isi workspace", w)
		}
		return "", fmt.Errorf("cwd tidak bisa diakses: %s: %v", w, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("cwd bukan direktori: %s. Gunakan list_dir untuk melihat isi workspace", w)
	}
	return abs, nil
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

	// 1. Exact match (fast path)
	if n := strings.Count(s, oldStr); n == 1 {
		return os.WriteFile(abs, []byte(strings.Replace(s, oldStr, newStr, 1)), 0o644)
	} else if n > 1 {
		return fmt.Errorf("old_text found %d times; provide more context to make it unique", n)
	}

	// 2. Line-based fuzzy match (ignores leading/trailing whitespace and line endings)
	contentLines := strings.Split(s, "\n")
	searchLines := strings.Split(strings.TrimSpace(oldStr), "\n")

	if len(searchLines) == 0 || (len(searchLines) == 1 && searchLines[0] == "") {
		return fmt.Errorf("old_text is empty")
	}

	var matchIdx = -1
	var matchesFound = 0

	for i := 0; i <= len(contentLines)-len(searchLines); i++ {
		match := true
		for j := 0; j < len(searchLines); j++ {
			cLine := strings.TrimSpace(contentLines[i+j])
			sLine := strings.TrimSpace(searchLines[j])
			if cLine != sLine {
				match = false
				break
			}
		}
		if match {
			matchesFound++
			matchIdx = i
		}
	}

	if matchesFound == 1 {
		preLines := contentLines[:matchIdx]
		postLines := contentLines[matchIdx+len(searchLines):]

		var result strings.Builder
		for _, l := range preLines {
			result.WriteString(l)
			result.WriteByte('\n')
		}
		result.WriteString(newStr)
		if len(postLines) > 0 {
			if !strings.HasSuffix(newStr, "\n") {
				result.WriteByte('\n')
			}
			for i, l := range postLines {
				result.WriteString(l)
				if i < len(postLines)-1 {
					result.WriteByte('\n')
				}
			}
		}
		return os.WriteFile(abs, []byte(result.String()), 0o644)
	}

	if matchesFound > 1 {
		return fmt.Errorf("fuzzy match found %d times; provide more context to make it unique", matchesFound)
	}

	return fmt.Errorf("old_text tidak ditemukan di %s. Pastikan teks benar-benar ada (termasuk indentasi dan spasi)", p)
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
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file tidak ditemukan: %s", p)
		}
		if os.IsPermission(err) {
			return "", fmt.Errorf("akses ditolak ke file: %s", p)
		}
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s. Use list_dir to see its contents", p)
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
	if info, err := os.Stat(abs); err == nil {
		if info.IsDir() {
			return fmt.Errorf("cannot write: %s is a directory. Use list_dir to see its contents", p)
		}
		if !overwrite {
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
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() && !entries[j].IsDir() {
			return true
		}
		if !entries[i].IsDir() && entries[j].IsDir() {
			return false
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	var sb strings.Builder
	maxEntries := 1000
	for i, e := range entries {
		if i >= maxEntries {
			sb.WriteString(fmt.Sprintf("\n... [truncated, %d more entries]", len(entries)-maxEntries))
			break
		}
		if e.IsDir() {
			sb.WriteString("DIR:  " + e.Name() + "\n")
		} else {
			info, err := e.Info()
			sizeStr := "unknown size"
			if err == nil {
				sizeStr = formatSize(info.Size())
			}
			sb.WriteString(fmt.Sprintf("FILE: %s (%s)\n", e.Name(), sizeStr))
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

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
