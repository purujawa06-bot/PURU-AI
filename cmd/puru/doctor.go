package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/config"
)

// doctorTools are the shell programs the agent relies on. Android ships a
// trimmed toybox: the report makes clear what is missing before a command
// silently fails in production.
var doctorTools = []string{"sh", "ls", "cat", "grep", "sed", "awk", "tar", "unzip", "curl", "wget", "git", "python3", "busybox", "puru"}

// runDoctor prints an environment report: config, workspace, shell and
// network. It is the first thing to run when a phone build misbehaves,
// because it separates "the bot is broken" from "this device cannot do X".
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to config.json")
	offline := fs.Bool("offline", false, "skip DNS/TLS checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected args: %s", strings.Join(fs.Args(), " "))
	}

	path := config.ResolvePath(*configPath)
	fmt.Printf("puru %s doctor\n", version)
	fmt.Printf("platform : %s/%s, %s, HOME=%s, TMPDIR=%s\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), os.Getenv("HOME"), os.Getenv("TMPDIR"))
	fmt.Printf("config   : %s\n", path)

	fails := 0
	check := func(name string, err error, detail string) {
		if err != nil {
			fails++
			fmt.Printf("FAIL %-10s %s: %v\n", name, detail, err)
			return
		}
		if detail == "" {
			fmt.Printf("OK   %-10s\n", name)
			return
		}
		fmt.Printf("OK   %-10s %s\n", name, detail)
	}

	cfg, err := config.Load(path)
	check("config", err, filepath.Base(path))
	var workspace string
	if err == nil {
		workspace = cfg.Workspace
		fmt.Printf("     workspace: %s\n", workspace)
		if cfg.RestrictWorkspace {
			fmt.Printf("     restrict_workspace=true: the agent can only read/write inside the workspace above\n")
		} else {
			fmt.Printf("     restrict_workspace=false: the agent may touch any path its OS user can write\n")
		}
		if host := baseURLHost(cfg.Model.BaseURL); host != "" {
			fmt.Printf("     model endpoint host: %s\n", host)
		}
	}

	if workspace != "" {
		check("workspace", checkWritableDir(workspace), workspace)
	}
	check("home", checkWritableDir(os.Getenv("HOME")), "puru can write here")
	check("tmp", checkWritableDir(os.TempDir()), os.TempDir())

	check("shell", checkShell(), shellLine())
	fmt.Printf("     tools found:")
	found, missing := doctorToolReport(doctorTools)
	fmt.Printf(" %s\n", strings.Join(found, " "))
	if len(missing) > 0 {
		fmt.Printf("     tools missing: %s (use `puru fetch -o $HOME/bin/<name> <url>` to install a static build)\n", strings.Join(missing, " "))
	}

	if *offline {
		fmt.Printf("     dns/tls checks skipped (-offline)\n")
	} else {
		hosts := doctorHosts(cfg)
		for _, host := range hosts {
			started := time.Now()
			addrs, err := lookupHost(host)
			check("dns", err, fmt.Sprintf("%s -> %s in %s", host, strings.Join(addrs, ","), time.Since(started).Round(time.Millisecond)))
		}
		check("tls", checkTLS(), "https://api.telegram.org reachable")
	}

	fmt.Println()
	if fails > 0 {
		return fmt.Errorf("%d check(s) failed", fails)
	}
	fmt.Println("all checks passed")
	return nil
}

// doctorHosts are the hosts worth resolving: the model endpoint the user
// configured, plus Telegram, which every gateway needs.
func doctorHosts(cfg *config.Config) []string {
	hosts := []string{"api.telegram.org"}
	if cfg != nil {
		if host := baseURLHost(cfg.Model.BaseURL); host != "" && host != "api.telegram.org" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// baseURLHost extracts the host of a configured base_url ("" when unusable).
func baseURLHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// lookupHost resolves with a bounded timeout: a hung resolver must not hang
// the whole report.
func lookupHost(host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupHost(ctx, host)
}

// checkTLS does a real HTTPS request so DNS, routing, TLS and CA trust all
// have to work for it to pass. Any HTTP status counts as success.
func checkTLS() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.telegram.org/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "puru/"+version)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// checkWritableDir creates and deletes a probe file, which is the only honest
// way to know the agent will be able to write (and delete) its files.
func checkWritableDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(dir, ".puru-doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	return os.Remove(probe)
}

// checkShell runs the same `sh -c` path the exec tool uses.
func checkShell() error {
	shell, prefix := shellCommand()
	args := append(append([]string{}, prefix...), "exit 0")
	out, err := exec.Command(shell, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v (%s)", shell, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// shellCommand is the shell the exec tool spawns; kept here so the report
// matches runtime behaviour on Windows too.
func shellCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C"}
	}
	return "sh", []string{"-c"}
}

// shellLine shows the resolved shell and the PATH the agent will inherit.
func shellLine() string {
	shell, err := exec.LookPath(shellName())
	if err != nil {
		shell = shellName() + " (not on PATH!)"
	}
	path := os.Getenv("PATH")
	if len(path) > 300 {
		path = path[:300] + "…"
	}
	if path == "" {
		path = "(empty)"
	}
	return fmt.Sprintf("%s, PATH=%s", shell, path)
}

func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}
	return "sh"
}

// doctorToolReport splits the interesting tools into present and missing.
func doctorToolReport(names []string) (found []string, missing []string) {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			found = append(found, name)
			continue
		}
		missing = append(missing, name)
	}
	return found, missing
}
