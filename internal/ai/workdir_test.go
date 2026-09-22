package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolveWorkdir harus menolak cwd yang tak ada / bukan direktori dengan
// pesan jelas, agar exec tak gagal di cmd.Start dengan error chdir generik.
func TestResolveWorkdirValidatesDir(t *testing.T) {
	ws := t.TempDir()

	if _, err := resolveWorkdir(ws, true, "tak-ada"); err == nil ||
		!strings.Contains(err.Error(), "cwd tidak ditemukan") {
		t.Fatalf("cwd tak ada harus ditolak jelas, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(ws, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveWorkdir(ws, true, "f.txt"); err == nil ||
		!strings.Contains(err.Error(), "cwd bukan direktori") {
		t.Fatalf("cwd file harus ditolak jelas, got %v", err)
	}

	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveWorkdir(ws, true, "sub"); err != nil || got == "" {
		t.Fatalf("cwd subdir valid harus lolos: %v %q", err, got)
	}
	if got, err := resolveWorkdir(ws, true, ""); err != nil || got != ws {
		t.Fatalf("cwd kosong harus return workspace: %v %q", err, got)
	}
}
