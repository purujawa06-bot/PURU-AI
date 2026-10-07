package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolveWorkdir must reject a missing/non-directory cwd with a clear
// message, so exec doesn't fail inside cmd.Start with a generic chdir error.
func TestResolveWorkdirValidatesDir(t *testing.T) {
	ws := t.TempDir()

	if _, err := resolveWorkdir(ws, true, filepath.Join(ws, "tak-ada")); err == nil ||
		!strings.Contains(err.Error(), "cwd not found") {
		t.Fatalf("cwd tak ada harus ditolak jelas, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveWorkdir(ws, true, filepath.Join(ws, "f.txt")); err == nil ||
		!strings.Contains(err.Error(), "cwd is not a directory") {
		t.Fatalf("cwd file harus ditolak jelas, got %v", err)
	}

	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveWorkdir(ws, true, filepath.Join(ws, "sub")); err != nil || got == "" {
		t.Fatalf("cwd subdir valid harus lolos: %v %q", err, got)
	}
	if got, err := resolveWorkdir(ws, true, ""); err != nil || got != ws {
		t.Fatalf("cwd kosong harus return workspace: %v %q", err, got)
	}
}
