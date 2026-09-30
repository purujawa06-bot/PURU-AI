package prompt

import (
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed defaults/build-prompt.md
var defaultBuildPrompt string

// BuildPromptFilename is the file name seeded beside config.json.
const BuildPromptFilename = "build-prompt.md"

// DefaultBuildPromptDir returns $HOME/.puru (/root/.puru for root).
func DefaultBuildPromptDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/root"
	}
	return filepath.Join(home, ".puru")
}

// DefaultBuildPromptPath returns the default build-prompt.md path.
func DefaultBuildPromptPath() string {
	return filepath.Join(DefaultBuildPromptDir(), BuildPromptFilename)
}

// resolveBuildPromptPath applies the request override, falling back to default.
func resolveBuildPromptPath(requestPath string) string {
	if strings.TrimSpace(requestPath) != "" {
		return strings.TrimSpace(requestPath)
	}
	return DefaultBuildPromptPath()
}

// EnsureBuildPrompt seeds the build prompt file from the embedded default.
// Existing files are never overwritten, so user edits are safe.
// An empty path is a no-op so tests and legacy callers stay hermetic.
func EnsureBuildPrompt(requestPath string) error {
	if strings.TrimSpace(requestPath) == "" {
		return nil
	}
	path := resolveBuildPromptPath(requestPath)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultBuildPrompt), 0o644)
}

// loadBuildPromptText reads the template file, falling back to the embedded
// default when the file is missing, empty, or unreadable.
// An empty path returns the embedded default without touching disk.
func loadBuildPromptText(requestPath string) string {
	if strings.TrimSpace(requestPath) == "" {
		return defaultBuildPrompt
	}
	path := resolveBuildPromptPath(requestPath)
	// Best-effort seeding keeps the file transparent on disk for editing.
	_ = EnsureBuildPrompt(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if strings.TrimSpace(requestPath) != "" {
			log.Printf("[prompt] read template %s: %v, using embedded default", path, err)
		}
		return defaultBuildPrompt
	}
	if strings.TrimSpace(string(data)) == "" {
		log.Printf("[prompt] template %s is empty, using embedded default", path)
		return defaultBuildPrompt
	}
	return string(data)
}

// renderBuildPromptSection executes one named section from the template text.
// On parse or execute failure it retries with the embedded default so a
// broken user edit degrades gracefully instead of breaking the agent.
func renderBuildPromptSection(templateText, name string, data any) string {
	render := func(text string) (string, error) {
		parsed, err := template.New("build-prompt").Parse(text)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		if err := parsed.ExecuteTemplate(&sb, name, data); err != nil {
			return "", err
		}
		return strings.TrimSpace(sb.String()), nil
	}
	if out, err := render(templateText); err == nil {
		return out
	} else {
		log.Printf("[prompt] template section %q failed, trying embedded default: %v", name, err)
	}
	out, err := render(defaultBuildPrompt)
	if err != nil {
		log.Printf("[prompt] embedded template section %q failed: %v", name, err)
		return ""
	}
	return out
}
