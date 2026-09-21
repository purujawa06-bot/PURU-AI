package prompt

import (
	"strings"
	"testing"
)

func TestGetRendersMemory(t *testing.T) {
	out, err := Get("memory-x")
	if err != nil {
		t.Fatalf("template error: %v", err)
	}
	if !strings.Contains(out, "memory-x") {
		t.Fatalf("memory not injected")
	}
	for _, tool := range []string{"read_file", "write_file", "edit_file", "exec"} {
		if !strings.Contains(out, tool) {
			t.Fatalf("tool %s missing in prompt", tool)
		}
	}
	if strings.Contains(out, "e2b") || strings.Contains(out, "skills") {
		t.Fatalf("old tool references must be gone: %s", out)
	}
}
