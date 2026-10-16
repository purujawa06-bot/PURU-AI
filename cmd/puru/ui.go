// Display layer for the puru CLI: ANSI styling, layout helpers, and the
// interactive prompt functions used by the setup wizard.
//
// Dependency-free on purpose: a wizard needs a handful of escape sequences,
// not a TUI library. Styling degrades to plain text when NO_COLOR is set,
// TERM=dumb, or stdout is not a terminal, so piped and container output
// stays clean.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	// ansiAccent is a 256-color orange used for headings, markers, and rules.
	ansiAccent = "\x1b[38;5;209m"

	// dividerWidth is the fixed width of section rules; terminal width would
	// need per-platform ioctl plumbing for a purely cosmetic gain.
	dividerWidth = 56
)

// colorsEnabled is resolved once at startup for stdout.
var colorsEnabled = detectColors()

// detectColors follows the NO_COLOR / FORCE_COLOR conventions and enables ANSI
// output only for a real terminal.
func detectColors() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	enableVirtualTerminal() // Windows console: opt into ANSI processing.
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func paint(code, s string) string {
	if !colorsEnabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

func accent(s string) string { return paint(ansiBold+ansiAccent, s) }
func bold(s string) string   { return paint(ansiBold, s) }
func dim(s string) string    { return paint(ansiDim, s) }
func good(s string) string   { return paint(ansiBold+ansiGreen, s) }
func warn(s string) string   { return paint(ansiYellow, s) }
func bad(s string) string    { return paint(ansiBold+ansiRed, s) }

// banner prints the wizard title followed by a dimmed subtitle.
func banner(title, subtitle string) {
	fmt.Println()
	fmt.Println("  " + accent(title))
	if subtitle != "" {
		fmt.Println("  " + dim(subtitle))
	}
}

// section prints a titled rule, e.g. "── Telegram ─────────────".
func section(title string) {
	width := dividerWidth - utf8.RuneCountInString(title) - 4
	if width < 3 {
		width = 3
	}
	fmt.Println()
	fmt.Println("  " + accent("── "+title+" ") + dim(strings.Repeat("─", width)))
}

// hint prints an indented, dimmed note under a header or question.
func hint(format string, args ...any) {
	fmt.Printf("  %s %s\n", dim("·"), dim(fmt.Sprintf(format, args...)))
}

// warnf prints an indented warning line for invalid or skipped input.
func warnf(format string, args ...any) {
	fmt.Printf("  %s %s\n", warn("!"), fmt.Sprintf(format, args...))
}

// summary prints an aligned label/value panel, used to preview the values
// written to config.json.
func summary(title string, rows [][2]string) {
	section(title)
	width := 0
	for _, row := range rows {
		if n := utf8.RuneCountInString(row[0]); n > width {
			width = n
		}
	}
	for _, row := range rows {
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(row[0]))
		fmt.Printf("  %s  %s\n", dim(row[0]+pad), row[1])
	}
}

// done prints the closing success line and the suggested next command.
func done(message, next string) {
	fmt.Println()
	fmt.Printf("  %s %s\n", good("✓"), bold(message))
	if next != "" {
		fmt.Printf("    %s %s\n", dim("next:"), next)
	}
	fmt.Println()
}

// fatal prints a styled error to stderr and exits with status 1.
func fatal(command string, err error) {
	fmt.Fprintf(os.Stderr, "%s %s: %v\n", bad("✗"), command, err)
	os.Exit(1)
}

// ---------- value formatting ----------

// maskSecret hides the middle of a secret, keeping a short prefix and suffix
// so the user can confirm the value without printing it in full.
func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", 4) + s[len(s)-4:]
}

// secretText reports only whether an optional secret is set.
func secretText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not set"
	}
	return "set"
}

// boolText renders a switch value as on/off.
func boolText(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// listText renders a string list, or "(none)" when empty.
func listText(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}

// userIDsText renders the Telegram allowlist, or "everyone allowed" when empty.
func userIDsText(ids []int64) string {
	if len(ids) == 0 {
		return "everyone allowed"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ", ")
}

func int64ListText(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// ---------- prompts ----------

// question formats a prompt line: "? Label [default]: ".
func question(label, def string) string {
	if def == "" {
		return fmt.Sprintf("%s %s: ", accent("?"), bold(label))
	}
	return fmt.Sprintf("%s %s %s: ", accent("?"), bold(label), dim("["+def+"]"))
}

// readLine prints the prompt and returns one trimmed line. EOF is returned as
// an error so callers can fall back to defaults instead of blocking or looping.
func readLine(in *bufio.Reader, label, def string) (string, error) {
	fmt.Print(question(label, def))
	line, err := in.ReadString('\n')
	return strings.TrimSpace(line), err
}

// askLine returns the trimmed answer, or def on empty input. An empty answer
// for a required field without a default is re-asked until a value arrives or
// stdin ends.
func askLine(in *bufio.Reader, label, def string, required bool) string {
	for {
		line, err := readLine(in, label, def)
		if line != "" {
			return line
		}
		if !required || def != "" || err != nil {
			return def
		}
		warnf("this field is required")
	}
}

// askBool asks a yes/no question; empty input keeps def.
func askBool(in *bufio.Reader, label string, def bool) bool {
	defStr := "y/N"
	if def {
		defStr = "Y/n"
	}
	line, _ := readLine(in, label, defStr)
	return normalizeYes(line, def)
}

// askInt returns the parsed integer, or def when the input is empty or invalid.
func askInt(in *bufio.Reader, label string, def int) int {
	line, _ := readLine(in, label, strconv.Itoa(def))
	if line == "" {
		return def
	}
	n, err := strconv.Atoi(line)
	if err != nil {
		warnf("invalid number %q, keeping default %d", line, def)
		return def
	}
	return n
}

// askFloat returns the parsed float, or def when the input is empty or invalid.
func askFloat(in *bufio.Reader, label string, def float64) float64 {
	line, _ := readLine(in, label, strconv.FormatFloat(def, 'g', -1, 64))
	if line == "" {
		return def
	}
	f, err := strconv.ParseFloat(line, 64)
	if err != nil {
		warnf("invalid number %q, keeping default %g", line, def)
		return def
	}
	return f
}

// askInt64List parses a comma-separated id list; empty input keeps def.
func askInt64List(in *bufio.Reader, label string, def []int64) []int64 {
	line, _ := readLine(in, label, int64ListText(def))
	if line == "" {
		return def
	}
	out := []int64{}
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			warnf("skipping invalid id %q", part)
			continue
		}
		out = append(out, n)
	}
	return out
}

// askStringList parses a comma-separated list; empty input keeps def.
func askStringList(in *bufio.Reader, label string, def []string) []string {
	line, _ := readLine(in, label, strings.Join(def, ","))
	if line == "" {
		return def
	}
	out := []string{}
	for _, part := range strings.Split(line, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// askSkillsMode asks for the skills mode, keeping def on unknown input.
func askSkillsMode(in *bufio.Reader, def string) string {
	line, _ := readLine(in, "Skills mode (default/off/custom)", def)
	line = strings.ToLower(line)
	if line == "" {
		return def
	}
	switch line {
	case "default", "off", "custom":
		return line
	default:
		warnf("unknown skills_mode, keeping default %s", def)
		return def
	}
}
