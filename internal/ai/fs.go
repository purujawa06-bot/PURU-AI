// Package ai: lightweight local assistant agent.
//
// Tools (9, all jailed to Workspace when
// Config.RestrictWorkspace is true) — declarations mirror picoclaw:
//   - read_file (path, offset, length), write_file (path, content, overwrite),
//     list_dir (path), edit_file_replace_string (path, old_text, new_text),
//     edit_file_replace_line (path, start_line, end_line, new_text),
//     edit_file_apply_patch (path, patch), exec (action
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
		return "", fmt.Errorf("path is empty")
	}
	if restrict {
		if filepath.IsAbs(p) {
			// Allow absolute paths only when already inside workspace.
			abs := filepath.Clean(p)
			if !insideDir(abs, workspace) {
				return "", fmt.Errorf("outside workspace: %q", p)
			}
			return abs, nil
		}
		joined := filepath.Join(workspace, filepath.FromSlash(p))
		abs, err := filepath.Abs(joined)
		if err != nil {
			return "", err
		}
		if !insideDir(abs, workspace) {
			return "", fmt.Errorf("outside workspace: %q", p)
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
				return "", fmt.Errorf("workspace not found: %s", workspace)
			}
			return "", fmt.Errorf("workspace is not accessible: %s: %v", workspace, err)
		} else if !st.IsDir() {
			return "", fmt.Errorf("workspace is not a directory: %s", workspace)
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
			return "", fmt.Errorf("cwd not found: %s. Use list_dir to see workspace contents", w)
		}
		return "", fmt.Errorf("cwd is not accessible: %s: %v", w, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("cwd is not a directory: %s. Use list_dir to see workspace contents", w)
	}
	return abs, nil
}

// editLocalFileByLine replaces lines [startLine, endLine] (1-based, inclusive)
// in a file with newText (may span lines; empty deletes the range).
// The file's trailing newline is preserved.
func editLocalFileByLine(workspace string, restrict bool, p string, startLine, endLine int64, newText string) error {
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	s := string(b)
	trailingNL := strings.HasSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	if trailingNL {
		lines = lines[:len(lines)-1]
	}
	n := int64(len(lines))
	if startLine < 1 || endLine < startLine || startLine > n || endLine > n {
		return fmt.Errorf("line range %d-%d out of bounds (file %s has %d lines)", startLine, endLine, p, n)
	}
	var newLines []string
	if newText != "" {
		newLines = strings.Split(newText, "\n")
	}
	out := make([]string, 0, len(lines)-(int(endLine-startLine)+1)+len(newLines))
	out = append(out, lines[:startLine-1]...)
	out = append(out, newLines...)
	out = append(out, lines[endLine:]...)
	res := strings.Join(out, "\n")
	if trailingNL && (len(res) > 0 || len(out) > 0) {
		res += "\n"
	}
	return os.WriteFile(abs, []byte(res), 0o644)
}

// patchHunk is one @@ hunk of a unified diff.
type patchHunk struct {
	oldStart int // 1-based; 0 = insert before line 1
	lines    []patchLine
}

type patchLine struct {
	kind byte // ' ', '-', '+'
	text string
}

// parseHunkHeader parses "@@ -oldStart[,oldCount] +newStart[,newCount] @@".
// Declared counts are informational only (models often miscount); only the
// oldStart position is used, context lines are verified strictly.
func parseHunkHeader(line string) (int, bool) {
	if !strings.HasPrefix(line, "@@") {
		return 0, false
	}
	rest := strings.TrimPrefix(line, "@@")
	end := strings.Index(rest, "@@")
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(strings.TrimSpace(rest[:end]))
	if len(fields) < 2 {
		return 0, false
	}
	oldRange := strings.TrimPrefix(fields[0], "-")
	if i := strings.Index(oldRange, ","); i >= 0 {
		oldRange = oldRange[:i]
	}
	var start int
	if _, err := fmt.Sscanf(oldRange, "%d", &start); err != nil {
		return 0, false
	}
	return start, true
}

// parseUnifiedPatch parses a git-style unified diff into hunks. File headers
// (diff --git, index, ---, +++) and "\ No newline" markers are skipped.
func parseUnifiedPatch(patch string) ([]patchHunk, error) {
	var hunks []patchHunk
	var cur *patchHunk
	for _, raw := range strings.Split(patch, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		switch {
		case strings.HasPrefix(line, "@@"):
			start, ok := parseHunkHeader(line)
			if !ok {
				return nil, fmt.Errorf("malformed hunk header: %q", truncLine(line))
			}
			hunks = append(hunks, patchHunk{oldStart: start})
			cur = &hunks[len(hunks)-1]
		case strings.HasPrefix(line, "diff --git"),
			strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "--- "),
			strings.HasPrefix(line, "+++ "),
			strings.HasPrefix(line, `\ `):
			continue
		case cur == nil:
			if strings.TrimSpace(line) == "" {
				continue
			}
			return nil, fmt.Errorf("patch line outside hunk: %q (patch must be a unified diff with @@ hunks)", truncLine(line))
		case strings.HasPrefix(line, " "), strings.HasPrefix(line, "-"), strings.HasPrefix(line, "+"):
			cur.lines = append(cur.lines, patchLine{kind: line[0], text: line[1:]})
		case strings.TrimSpace(line) == "":
			continue
		default:
			return nil, fmt.Errorf("malformed patch line: %q (hunk lines must start with ' ', '-' or '+')", truncLine(line))
		}
	}
	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks found; patch must be a unified diff with @@ hunks")
	}
	for i, h := range hunks {
		if len(h.lines) == 0 {
			return nil, fmt.Errorf("hunk %d is empty", i+1)
		}
	}
	return hunks, nil
}

func truncLine(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// applyUnifiedPatch applies hunks to original content, verifying every
// context (' ') and removal ('-') line matches exactly. Hunks must be in
// ascending, non-overlapping order (standard unified-diff semantics: oldStart
// refers to positions in the original file).
func applyUnifiedPatch(original string, hunks []patchHunk) (string, error) {
	trailingNL := strings.HasSuffix(original, "\n")
	orig := strings.Split(original, "\n")
	if trailingNL {
		orig = orig[:len(orig)-1]
	}
	var out []string
	added, removed := 0, 0
	cursor := 0 // next unconsumed index in orig (0-based)
	for i, h := range hunks {
		pos := h.oldStart - 1 // 0-based; oldStart=0 inserts before line 1
		if h.oldStart == 0 {
			pos = 0
		}
		if pos < cursor {
			return "", fmt.Errorf("hunk %d overlaps a previous hunk (hunks must be ascending)", i+1)
		}
		if pos > len(orig) {
			return "", fmt.Errorf("hunk %d starts at line %d but file has %d lines", i+1, h.oldStart, len(orig))
		}
		out = append(out, orig[cursor:pos]...)
		cursor = pos
		for _, pl := range h.lines {
			switch pl.kind {
			case ' ', '-':
				if cursor >= len(orig) {
					return "", fmt.Errorf("hunk %d: expected %q past end of file (%d lines)", i+1, truncLine(pl.text), len(orig))
				}
				if orig[cursor] != pl.text {
					return "", fmt.Errorf("hunk %d context mismatch at file line %d: expected %q, found %q", i+1, cursor+1, truncLine(pl.text), truncLine(orig[cursor]))
				}
				if pl.kind == ' ' {
					out = append(out, orig[cursor])
				} else {
					removed++
				}
				cursor++
			case '+':
				out = append(out, pl.text)
				added++
			}
		}
	}
	out = append(out, orig[cursor:]...)
	res := strings.Join(out, "\n")
	if trailingNL && len(out) > 0 {
		res += "\n"
	}
	return res, nil
}

// editLocalFileApplyPatch applies a git-style unified-diff patch to a file.
// Returns a short stat summary for the model.
func editLocalFileApplyPatch(workspace string, restrict bool, p, patch string) (string, error) {
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	hunks, err := parseUnifiedPatch(patch)
	if err != nil {
		return "", err
	}
	res, err := applyUnifiedPatch(string(b), hunks)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(res), 0o644); err != nil {
		return "", err
	}
	added, removed := 0, 0
	for _, h := range hunks {
		for _, pl := range h.lines {
			if pl.kind == '+' {
				added++
			} else if pl.kind == '-' {
				removed++
			}
		}
	}
	return fmt.Sprintf("Patch applied: %s (%d hunk(s), +%d/-%d lines)", p, len(hunks), added, removed), nil
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

	return fmt.Errorf("old_text not found in %s. Make sure the text exists exactly (including indentation and spacing)", p)
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
			return "", fmt.Errorf("file not found: %s", p)
		}
		if os.IsPermission(err) {
			return "", fmt.Errorf("access denied to file: %s", p)
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
// a hint to use append_file/edit_file_* instead.
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
			return fmt.Errorf("file: %s already exists. To add to it or change part of it without losing the current contents, use append_file, edit_file_replace_string, edit_file_replace_line, or edit_file_apply_patch. Only set overwrite=true if you intend to replace the entire file.", p)
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
