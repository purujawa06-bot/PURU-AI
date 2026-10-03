// Package ai: lightweight local assistant agent.
//
// Tools (all jailed to Workspace when
// Config.RestrictWorkspace is true) — declarations mirror picoclaw:
//   - read_file (path, start_line, length), write_file (path, content, overwrite),
//     list_dir (path), edit_file (path, old_string, new_string),
//     run_shell_command (action
//     wajib: run/list/poll/read/kill; command, sessionId, background, cwd,
//     timeout opsional)
//   - telegram_sendfile, telegram_getuser (need a Telegram request context)
//   - get_env (environment info)
//
// No VFS, no sandbox, no web, no fallback, no skills.
package ai

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const maxExecOutput = 20_000

// defaultReadFileLines and maxReadFileLines cap one read_file call
// (line-based pagination, anti context-overflow).
const (
	defaultReadFileLines = 200
	maxReadFileLines     = 2000
)

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
		return fmt.Errorf("old_string found %d times; provide more context to make it unique", n)
	}

	// 2. Line-based fuzzy match (ignores leading/trailing whitespace and line endings)
	contentLines := strings.Split(s, "\n")
	searchLines := strings.Split(strings.TrimSpace(oldStr), "\n")

	if len(searchLines) == 0 || (len(searchLines) == 1 && searchLines[0] == "") {
		return fmt.Errorf("old_string is empty")
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

	return fmt.Errorf("old_string not found in %s. Make sure the text exists exactly (including indentation and spacing)", p)
}

// readLocalFile reads path with line pagination.
// Returns the full LLM text: "[file: base | total: N lines | read: lines a-b]"
// header + "[TRUNCATED ... start_line=X ...]" / "[END OF FILE ...]" trailer, or
// "[END OF FILE - no content at this start_line]" when start_line is past the end.
func readLocalFile(workspace string, restrict bool, p string, startLine, length int64) (string, error) {
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return "", err
	}
	if startLine <= 0 {
		startLine = 1
	}
	if length <= 0 {
		return "", fmt.Errorf("length must be > 0")
	}
	if length > maxReadFileLines {
		length = maxReadFileLines
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
	st, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("path is a directory: %s. Use list_dir to see its contents", p)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	var all []string
	for sc.Scan() {
		line := sc.Text()
		if strings.ContainsRune(line, 0) {
			return "", fmt.Errorf("file appears to be binary")
		}
		all = append(all, line)
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("failed to read file content: %w", err)
	}
	total := len(all)
	if total == 0 || startLine > int64(total) {
		return "[END OF FILE - no content at this start_line]", nil
	}
	startIdx := int(startLine - 1)
	endIdx := startIdx + int(length)
	if endIdx > total {
		endIdx = total
	}
	page := all[startIdx:endIdx]
	data := strings.Join(page, "\n")
	readEnd := int64(startIdx + len(page))
	header := fmt.Sprintf("[file: %s | total: %d lines | read: lines %d-%d]",
		filepath.Base(p), total, startLine, readEnd)
	if endIdx < total {
		header += fmt.Sprintf("\n[TRUNCATED - file has more content. Call read_file again with start_line=%d to continue.]", endIdx+1)
	} else {
		header += "\n[END OF FILE - no further content.]"
	}
	return header + "\n\n" + data, nil
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
			return fmt.Errorf("file %q already exists with overwrite=false. To replace the whole file pass overwrite=true, for example {\"path\": %q, \"content\": \"...\", \"overwrite\": true}. To keep the current contents, use append_file to add or edit_file for partial changes.", p, p)
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

// grepLocal searches a file or a directory tree for a literal keyword.
// path may point at one file (single-file search) or a directory
// (recursive walk including subfolders). ext optionally filters by
// extension, e.g. "go" or ".go,.md" (empty = all files).
// Returns "rel:line: text" lines, or "(no matches)" when nothing matches.
func grepLocal(workspace string, restrict bool, p, keyword, ext string, maxResults int) (string, error) {
	if strings.TrimSpace(keyword) == "" {
		return "", fmt.Errorf("keyword is required")
	}
	if strings.TrimSpace(p) == "" {
		p = "."
	}
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}
	abs, err := resolvePath(workspace, restrict, p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("path not found: %s", p)
		}
		return "", fmt.Errorf("cannot access path: %w", err)
	}
	exts := parseGrepExt(ext)
	var files []string
	if !st.IsDir() {
		files = []string{abs}
	} else {
		err = filepath.WalkDir(abs, func(fp string, d os.DirEntry, werr error) error {
			if werr != nil {
				return nil // skip unreadable, keep going
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if len(exts) > 0 && !exts[strings.ToLower(filepath.Ext(d.Name()))] {
				return nil
			}
			if restrict && !insideDir(fp, workspace) {
				return nil
			}
			if info, err := d.Info(); err == nil && info.Size() > 2<<20 {
				return nil // skip files >2MB, read_file covers them
			}
			files = append(files, fp)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("failed to walk directory: %w", err)
		}
		sort.Strings(files)
	}
	var sb strings.Builder
	matched := 0
	truncated := false
	for _, fp := range files {
		if len(exts) > 0 && !st.IsDir() && !exts[strings.ToLower(filepath.Ext(fp))] {
			continue
		}
		n, done, _ := grepOneFile(&sb, workspace, fp, keyword, maxResults-matched)
		matched += n
		if done {
			truncated = true
			break
		}
	}
	if matched == 0 {
		return "(no matches)", nil
	}
	out := sb.String()
	if truncated {
		out += fmt.Sprintf("... [truncated at %d matches, narrow path/ext/keyword]\n", maxResults)
	}
	return out, nil
}

// parseGrepExt normalizes "go" / ".go,.md" into a set like {".go":true}.
func parseGrepExt(ext string) map[string]bool {
	out := map[string]bool{}
	for _, e := range strings.Split(ext, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out[e] = true
	}
	return out
}

// grepOneFile appends "rel:line: text" hits from one file.
// Returns (newHits, hitLimit, skippedBinary).
func grepOneFile(sb *strings.Builder, workspace, fp, keyword string, budget int) (int, bool, bool) {
	f, err := os.Open(fp)
	if err != nil {
		return 0, false, false
	}
	defer f.Close()
	rel := fp
	if workspace != "" {
		if r, err := filepath.Rel(workspace, fp); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
			rel = filepath.ToSlash(r)
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	hits := 0
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.ContainsRune(text, 0) {
			return hits, false, true // binary file, skip silently
		}
		if !strings.Contains(text, keyword) {
			continue
		}
		if len(text) > 500 {
			text = text[:500] + "…"
		}
		sb.WriteString(fmt.Sprintf("%s:%d: %s\n", rel, line, text))
		hits++
		if hits >= budget {
			return hits, true, false
		}
	}
	return hits, false, false
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
