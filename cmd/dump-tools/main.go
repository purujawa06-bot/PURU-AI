// Command dump-tools runs representative tool calls and writes one markdown
// per tool (e.g. read_file-output.md). Debug helper for CI: no local Go
// toolchain needed to inspect real tool outputs.
//
// Usage:
//
//	go run ./cmd/dump-tools [--out tool-dump]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

type dumpCase struct {
	tool string
	args map[string]any
}

func main() {
	outDir := flag.String("out", "tool-dump", "output dir for *-output.md files")
	flag.Parse()

	ws, err := os.MkdirTemp("", "puru-tools-dump-")
	if err != nil {
		log.Fatalf("tmp: %v", err)
	}
	seeds := map[string]string{
		"hello.txt":      "hello world\nline two\nline three\n",
		"editme.txt":     "foo world bar\n",
		"appendme.txt":   "start\n",
		"sub/nested.txt": "nested hello\n",
	}
	for p, c := range seeds {
		fp := filepath.Join(ws, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			log.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fp, []byte(c), 0o644); err != nil {
			log.Fatalf("seed: %v", err)
		}
	}

	a := &ai.Agent{Config: &config.Config{Workspace: ws, RestrictWorkspace: true}}
	tools := ai.BuildTools(a, nil)
	cases := []dumpCase{
		{"read_file", map[string]any{"path": "hello.txt"}},
		{"write_file", map[string]any{"path": "written.txt", "content": "hi\nthere\n", "overwrite": true}},
		{"list_dir", map[string]any{"path": "."}},
		{"edit_file", map[string]any{"path": "editme.txt", "old_string": "world", "new_string": "Puru"}},
		{"edit_file_by_line", map[string]any{"path": "hello.txt", "start_line": 2, "content": "LINE-DUA"}},
		{"append_file", map[string]any{"path": "appendme.txt", "content": "\nappended"}},
		{"run_shell_command", map[string]any{"action": "run", "command": "echo hi", "timeout": 30}},
		{"get_env", map[string]any{}},
		{"manage_schedule", map[string]any{"action": "list"}},
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("out: %v", err)
	}
	ctx := context.Background()
	for _, c := range cases {
		t := tools[c.tool]
		if t == nil {
			log.Printf("skip %s: no tool", c.tool)
			continue
		}
		res, runErr := t.Run(ctx, c.args)
		var body string
		if runErr != nil {
			body = "GO ERROR: " + runErr.Error()
		} else if s, ok := res.(string); ok {
			body = s
		} else {
			b, _ := json.MarshalIndent(res, "", "  ")
			body = string(b)
		}
		argsJSON, _ := json.MarshalIndent(c.args, "", "  ")
		md := fmt.Sprintf("# %s output\n\n> %s\n\n## args\n\n```json\n%s\n```\n\n## output\n\n```\n%s\n```\n", c.tool, t.Description, argsJSON, body)
		fp := filepath.Join(*outDir, c.tool+"-output.md")
		if err := os.WriteFile(fp, []byte(md), 0o644); err != nil {
			log.Fatalf("write %s: %v", fp, err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", fp, len(md))
	}
}
