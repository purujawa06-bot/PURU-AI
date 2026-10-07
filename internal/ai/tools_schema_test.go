package ai

import "testing"

// allParamsDesc + allToolsDesc are the single source of truth.
// BuildTools must serve exactly what is in toolSchemas.
func TestToolSchemaJSONSingleSource(t *testing.T) {
	all, err := loadToolSchemas()
	if err != nil {
		t.Fatalf("load schemas: %v", err)
	}
	tools := BuildTools(testAgent(t.TempDir()), nil)
	for name, tool := range tools {
		e, ok := all[name]
		if !ok {
			t.Errorf("tool %s has no schema", name)
			continue
		}
		if tool.Description != e.Description {
			t.Errorf("tool %s description drift", name)
		}
		if tool.Parameters == nil {
			t.Errorf("tool %s parameters nil", name)
		}
	}
	// Every entry must be consumed by a real tool (no stale entries).
	for name := range all {
		if tools[name] == nil {
			// web_search is opt-in; allow it to exist without a default tool.
			if name == "web_search" {
				continue
			}
			t.Errorf("schema %s has no tool", name)
		}
	}
	// Opt-in web_search must also come from schemas when enabled.
	searchAgent := testAgent(t.TempDir())
	searchAgent.Config.WebSearch.AIStudio.Active = true
	searchAgent.Config.WebSearch.AIStudio.Model = "gemini-2.5-flash"
	searchAgent.Config.WebSearch.AIStudio.APIKey = "test-key"
	enabled := BuildTools(searchAgent, nil)
	ws := enabled["web_search"]
	if ws == nil {
		t.Fatalf("web_search missing when enabled")
	}
	if ws.Description != all["web_search"].Description {
		t.Errorf("web_search description drift")
	}
}
