package ai

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// tools_schema.json is the single source of truth for every tool
// description and parameter schema. Edit that file to change what the
// model sees — mk(name, run) in tools.go loads desc/params from here.
//go:embed tools_schema.json
var rawToolSchema []byte

// toolSchemaEntry is one tool entry from tools_schema.json.
type toolSchemaEntry struct {
	Description string         `json:"description"`
	Required    []string       `json:"required"`
	Properties  map[string]any `json:"properties"`
}

var (
	schemaOnce  sync.Once
	schemaCache map[string]toolSchemaEntry
	schemaErr   error
)

// loadToolSchemas parses the embedded JSON once per process.
func loadToolSchemas() (map[string]toolSchemaEntry, error) {
	schemaOnce.Do(func() {
		var root struct {
			Tools map[string]toolSchemaEntry `json:"tools"`
		}
		if err := json.Unmarshal(rawToolSchema, &root); err != nil {
			schemaErr = fmt.Errorf("parse tools_schema.json: %w", err)
			return
		}
		if len(root.Tools) == 0 {
			schemaErr = fmt.Errorf("tools_schema.json has no tools")
			return
		}
		schemaCache = root.Tools
	})
	return schemaCache, schemaErr
}

// mustEntry fetches one tool entry and panics when it is missing or has an
// empty description, so a typo in tools_schema.json fails at build time
// (go test) instead of silently shipping a tool the model cannot use.
func mustEntry(name string) toolSchemaEntry {
	all, err := loadToolSchemas()
	if err != nil {
		panic(err)
	}
	e, ok := all[name]
	if !ok {
		panic(fmt.Sprintf("tool %q missing in tools_schema.json", name))
	}
	if e.Description == "" {
		panic(fmt.Sprintf("tool %q has empty description in tools_schema.json", name))
	}
	return e
}

// toolDescription returns the description for name from tools_schema.json.
func toolDescription(name string) string {
	return mustEntry(name).Description
}

// toolParameters returns a fresh params object for name:
// {type:"object", properties:{...}, required:[...] when non-empty}.
// Copied per call so each BuildTools gets its own map — the agent layer
// mutates these (sanitizeParams) when building function definitions.
func toolParameters(name string) map[string]any {
	e := mustEntry(name)
	props := make(map[string]any, len(e.Properties))
	for k, v := range e.Properties {
		props[k] = v
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(e.Required) > 0 {
		req := make([]string, len(e.Required))
		copy(req, e.Required)
		out["required"] = req
	}
	return out
}
