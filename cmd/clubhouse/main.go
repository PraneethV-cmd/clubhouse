// Command clubhouse is a multiplayer coordination layer for teams running Codex
// in the same repo. It runs as a coordinator, an MCP server Codex connects to,
// and PreToolUse hooks that hard-enforce file locks.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"

	"clubhouse/internal/client"
	"clubhouse/internal/clog"
	"clubhouse/internal/config"
	"clubhouse/internal/hook"
	"clubhouse/internal/menu"
	"clubhouse/internal/room"
	"clubhouse/internal/server"
	"clubhouse/internal/setup"
	"clubhouse/internal/ui"
)

var (
	pidPath    = config.Dir + "/serve.pid"
	logPath    = config.Dir + "/serve.log"
	invitePath = config.Dir + "/invite"
)

func main() {
	if len(os.Args) < 2 {
		runWatch() // bare `clubhouse` opens the dashboard
		return
	}
	switch os.Args[1] {
	case "open", "serve":
		cmdServe(os.Args[2:])
	case "__serve": // internal: the detached coordinator process
		cmdServeChild(os.Args[2:])
	case "close":
		cmdClose(os.Args[2:])
	case "enter", "join":
		cmdJoin(os.Args[2:])
	case "invite":
		cmdInvite()
	case "host":
		cmdHost(os.Args[2:])
	case "check":
		cmdCheck(os.Args[2:])
	case "menu":
		cmdMenu(os.Args[2:])
	case "hook":
		cmdHook(os.Args[2:])
	case "setup":
		if err := setup.Setup(); err != nil {
			clog.Stderr().Fatal("setup failed", "err", err)
		}
		fmt.Println(ui.Banner())
		fmt.Println("   " + ui.OK("wired into Codex: hooks, tools, and skill installed"))
		fmt.Println("   " + ui.Hint("next: run `codex`, then ask \"who's in the clubhouse?\""))
	case "unsetup":
		if err := setup.Unsetup(); err != nil {
			clog.Stderr().Fatal("unsetup failed", "err", err)
		}
		fmt.Println("removed clubhouse from Codex.")
	case "mcp":
		runMCP()
	case "lounge", "watch":
		runWatch()
	default:
		fmt.Println("usage: clubhouse [host|open|close|enter|invite|lounge|check|menu]  (no args = lounge)")
	}
}

// cmdServe launches the coordinator as a detached background process.
func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	rotate := fs.Duration("rotate", 10*time.Minute, "invite link rotation interval")
	fs.Parse(args)

	if pid := readPid(); pid > 0 && alive(pid) {
		clog.Stderr().Info("clubhouse already open", "pid", pid)
		printInvite()
		return
	} else if pid > 0 {
		os.Remove(pidPath)
	}
	if invite, err := existingCoordinatorInvite(*addr); err == nil {
		cfg := config.LoadOrDefault()
		cfg.Addr = *addr
		cfg.Server = "http://" + *addr
		if err := cfg.Save(); err != nil {
			clog.Stderr().Fatal("could not save clubhouse config", "err", err)
		}
		os.MkdirAll(config.Dir, 0o755)
		os.WriteFile(invitePath, []byte(invite), 0o644)
		fmt.Println(ui.Banner())
		fmt.Println("   " + ui.OK("clubhouse already open on "+*addr))
		fmt.Println("   invite ▸ " + ui.Link(invite))
		fmt.Println("   " + ui.Hint("watch with `clubhouse lounge`; if this is stale, run `clubhouse close --port`"))
		return
	}
	os.MkdirAll(config.Dir, 0o755)
	os.Remove(invitePath)
	logf, err := os.Create(logPath)
	if err != nil {
		clog.Stderr().Fatal("could not open daemon log", "err", err)
	}
	cfg := config.LoadOrDefault()
	cfg.Addr = *addr
	cfg.Server = "http://" + *addr
	cfg.EnsureRoom()
	if err := cfg.Save(); err != nil {
		clog.Stderr().Fatal("could not save clubhouse config", "err", err)
	}
	self, _ := os.Executable()
	cmd := exec.Command(self, "__serve", "--addr", *addr, "--rotate", rotate.String())
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive parent exit
	if err := cmd.Start(); err != nil {
		clog.Stderr().Fatal("could not open clubhouse", "err", err)
	}
	logf.Close()
	os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)

	invite, err := waitForInvite(cmd, config.ReadToken(), 3*time.Second)
	if err != nil {
		os.Remove(pidPath)
		os.Remove(invitePath)
		clog.Stderr().Fatal("could not open clubhouse", "err", err, "log", logPath)
	}
	fmt.Println(ui.Banner())
	fmt.Printf("   %s (pid %d)\n", ui.OK("running in the background"), cmd.Process.Pid)
	fmt.Println("   invite ▸ " + ui.Link(invite))
	fmt.Println("   " + ui.Hint("share the invite; close with `clubhouse close`; watch with `clubhouse lounge`"))
}

// cmdServeChild is the actual coordinator; it runs in the foreground of the
// detached process spawned by cmdServe.
func cmdServeChild(args []string) {
	fs := flag.NewFlagSet("__serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8787", "")
	rotate := fs.Duration("rotate", 10*time.Minute, "")
	fs.Parse(args)

	cfg := config.LoadOrDefault()
	name := firstNonEmpty(cfg.Room, config.DefaultRoomName(), "the-clubhouse")
	token := config.LoadOrCreateToken()
	server.SetRotate(*rotate)
	s := server.New(name, token, config.SnapPath, "http://"+*addr)
	clog.Stderr().Fatal("server stopped", "err", s.ListenAndServe(*addr))
}

func cmdClose(args []string) {
	if len(args) > 0 && args[0] == "--port" {
		if err := killPort("127.0.0.1:8787"); err != nil {
			clog.Stderr().Fatal("could not close clubhouse on port", "err", err)
		}
		os.Remove(pidPath)
		os.Remove(invitePath)
		clog.Stderr().Info("clubhouse port closed")
		return
	}
	pid := readPid()
	if pid <= 0 || !alive(pid) {
		fmt.Println("no clubhouse running here")
		os.Remove(pidPath)
		return
	}
	syscall.Kill(pid, syscall.SIGTERM)
	os.Remove(pidPath)
	os.Remove(invitePath)
	clog.Stderr().Info("clubhouse closed")
}

func existingCoordinatorInvite(addr string) (string, error) {
	token := config.ReadToken()
	if token == "" {
		return "", errors.New("no local room token")
	}
	return client.New("http://"+addr, token).Invite()
}

func killPort(addr string) error {
	i := strings.LastIndex(addr, ":")
	if i < 0 || i == len(addr)-1 {
		return fmt.Errorf("bad address %q", addr)
	}
	port := addr[i+1:]
	out, err := exec.Command("lsof", "-tiTCP:"+port, "-sTCP:LISTEN").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("no clubhouse listener found on %s", addr)
	}
	var killed bool
	for _, pidText := range strings.Fields(string(out)) {
		cmdline, _ := exec.Command("ps", "-p", pidText, "-o", "command=").Output()
		if !strings.Contains(string(cmdline), "clubhouse __serve") {
			return fmt.Errorf("port %s is held by a non-clubhouse process: %s", addr, strings.TrimSpace(string(cmdline)))
		}
		pid, err := strconv.Atoi(pidText)
		if err != nil {
			return err
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return err
		}
		killed = true
	}
	if !killed {
		return fmt.Errorf("no clubhouse listener found on %s", addr)
	}
	return nil
}

func cmdJoin(args []string) {
	if len(args) == 0 {
		clog.Stderr().Fatal("usage: clubhouse enter rc://join/...")
	}
	srv, code, err := room.ParseInvite(args[0])
	if err != nil {
		clog.Stderr().Fatal("bad invite", "err", err)
	}
	token, err := client.Redeem(srv, code)
	if err != nil {
		clog.Stderr().Fatal("could not enter clubhouse", "err", err)
	}
	cfg := config.LoadOrDefault()
	cfg.Server = srv
	if err := cfg.Save(); err != nil {
		clog.Stderr().Fatal("could not save clubhouse config", "err", err)
	}
	if err := config.WriteToken(token); err != nil {
		clog.Stderr().Fatal("could not save room token", "err", err)
	}
	fmt.Println(ui.OK("joined " + srv))
	fmt.Println("   " + ui.Hint("next: clubhouse setup, then clubhouse lounge"))
}

func cmdInvite() {
	// host: read the local file the coordinator keeps fresh
	if b, err := os.ReadFile(invitePath); err == nil && len(b) > 0 {
		fmt.Println(strings.TrimSpace(string(b)))
		return
	}
	// member: ask the coordinator for the current link
	cfg := config.LoadOrDefault()
	token := config.ReadToken()
	if cfg.Server == "" || token == "" {
		clog.Stderr().Fatal("no clubhouse yet", "hint", "run `clubhouse open` or `clubhouse enter <invite>` first")
	}
	link, err := client.New(cfg.Server, token).Invite()
	if err != nil {
		clog.Stderr().Fatal("could not fetch invite", "err", err)
	}
	fmt.Println(link)
}

func cmdHost(args []string) {
	cfg := config.LoadOrDefault()
	memberName := cfg.Name
	roomName := firstNonEmpty(cfg.Room, config.DefaultRoomName())
	role := "host"
	invite := ""
	components := []string{"codex", "check"}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Your member name").
				Description("This is how you appear in the lounge. Leave blank for a generated handle.").
				Placeholder("turbo-otter").
				Value(&memberName),
			huh.NewInput().
				Title("Clubhouse name").
				Description("Used when hosting this repo. This is the room name shown in the lounge.").
				Placeholder("launch room").
				Value(&roomName),
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Options(
					huh.NewOption("Host this repo", "host"),
					huh.NewOption("Enter a friend's clubhouse", "enter"),
				).
				Value(&role),
			huh.NewInput().
				Title("Invite link").
				Description("Only needed when entering a friend's clubhouse.").
				Placeholder("rc://join/...").
				Value(&invite),
			huh.NewMultiSelect[string]().
				Title("Set up").
				Options(
					huh.NewOption("Codex hooks, MCP tools, and skill", "codex").Selected(true),
					huh.NewOption("Repair duplicate Codex config", "check").Selected(true),
					huh.NewOption("Open the local clubhouse server", "open"),
					huh.NewOption("Enter the lounge dashboard after setup", "lounge"),
				).
				Value(&components),
		),
	)
	if err := form.Run(); err != nil {
		clog.Stderr().Fatal("host setup cancelled", "err", err)
	}

	cfg.Name = strings.TrimSpace(memberName)
	if cfg.Name == "" {
		cfg.EnsureName()
	}
	if role == "host" {
		cfg.Room = strings.TrimSpace(roomName)
		if cfg.Room == "" {
			cfg.EnsureRoom()
		}
	}
	if err := cfg.Save(); err != nil {
		clog.Stderr().Fatal("could not save clubhouse config", "err", err)
	}

	if role == "enter" {
		if strings.TrimSpace(invite) == "" {
			clog.Stderr().Fatal("invite required", "hint", "paste an rc://join/... invite")
		}
		cmdJoin([]string{strings.TrimSpace(invite)})
	}
	if selected(components, "open") || role == "host" {
		cmdServe(nil)
	}
	if selected(components, "codex") {
		if err := setup.Setup(); err != nil {
			clog.Stderr().Fatal("setup failed", "err", err)
		}
		clog.Stderr().Info("Codex integration installed")
	}
	if selected(components, "check") {
		cmdCheck([]string{"--fix"})
	}
	if selected(components, "lounge") {
		runWatch()
	}
}

func cmdCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	fix := fs.Bool("fix", false, "repair clubhouse Codex config")
	fs.Parse(args)

	res, err := setup.Check(*fix)
	if err != nil {
		clog.Stderr().Fatal("check failed", "err", err)
	}
	log := clog.Stderr()
	if res.Fixed {
		log.Info("repaired Codex integration", "config", res.ConfigPath)
	}
	if len(res.Problems) == 0 {
		log.Info("Codex integration looks clean", "config", res.ConfigPath)
	} else {
		log.Warn("Codex integration needs attention", "config", res.ConfigPath)
		for _, p := range res.Problems {
			fmt.Println(" - " + p)
		}
	}
	if len(res.Warnings) > 0 {
		log.Warn("clubhouse environment has warnings")
		for _, w := range res.Warnings {
			fmt.Println(" ! " + w)
		}
	}
	if !*fix && len(res.Problems) > 0 {
		fmt.Println(ui.Hint("run `clubhouse check --fix` to rewrite the clubhouse hook and MCP block"))
	}
}

func cmdMenu(args []string) {
	fs := flag.NewFlagSet("menu", flag.ExitOnError)
	root := fs.String("root", ".", "codebase root to scan")
	out := fs.String("out", "menu", "directory for generated Markdown")
	tests := fs.Bool("tests", false, "include Go test files")
	clean := fs.Bool("clean", true, "remove files from the previous generated menu manifest")
	codex := fs.Bool("codex", false, "run Codex after scanning to enrich menu summaries")
	fs.Parse(args)
	if fs.NArg() > 0 {
		*root = fs.Arg(0)
	}

	res, err := menu.Generate(menu.Options{
		Root:         *root,
		Out:          *out,
		IncludeTests: *tests,
		Clean:        *clean,
	})
	if err != nil {
		clog.Stderr().Fatal("menu failed", "err", err)
	}
	fmt.Println(ui.OK(fmt.Sprintf("menu mapped %d packages, %d symbols, %d links", res.Packages, res.Symbols, res.Edges)))
	fmt.Println("   " + ui.Hint("open "+filepath.Join(res.Out, "index.md")+" in any Markdown viewer"))
	if *codex {
		if err := enrichMenuWithCodex(res); err != nil {
			clog.Stderr().Fatal("Codex menu enrichment failed", "err", err)
		}
	}
}

func enrichMenuWithCodex(res menu.Result) error {
	prompt := strings.Join([]string{
		"You are enriching a generated Clubhouse code menu.",
		"Read menu/index.md, menu/graph.md, package pages, symbol pages, and the source code.",
		"Edit only files under " + res.Out + ".",
		"Improve the \"What It Does\" sections with concise, factual summaries.",
		"Preserve front matter, wikilinks, normal Markdown links, source paths, Calls, and Called By sections.",
		"Do not edit source code. Do not invent behavior unsupported by the code.",
	}, "\n")
	cmd := exec.Command("codex", "exec", "--cd", res.Root, "--sandbox", "workspace-write", "--ask-for-approval", "never", prompt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func selected(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func cmdHook(args []string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "pre":
		hook.Pre()
	case "stop":
		hook.Stop()
	}
}

func printInvite() {
	if b, err := os.ReadFile(invitePath); err == nil && len(b) > 0 {
		fmt.Println("   invite ▸ " + ui.Link(strings.TrimSpace(string(b))))
	}
}

func waitForInvite(cmd *exec.Cmd, wantToken string, timeout time.Duration) (string, error) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	deadline := time.After(timeout)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	var lastErr error
	for {
		select {
		case err := <-done:
			if err == nil {
				err = errors.New("server process exited before it was ready")
			}
			return "", fmt.Errorf("%w: %s", err, tailFile(logPath, 1200))
		case <-deadline:
			cmd.Process.Signal(syscall.SIGTERM)
			if lastErr != nil {
				return "", fmt.Errorf("server did not become ready: %w", lastErr)
			}
			return "", errors.New("server did not publish a usable invite")
		case <-tick.C:
			invite := readInvite()
			if invite == "" {
				continue
			}
			serverURL, code, err := room.ParseInvite(invite)
			if err != nil {
				lastErr = err
				continue
			}
			token, err := client.Redeem(serverURL, code)
			if err != nil {
				lastErr = err
				continue
			}
			if wantToken != "" && token != wantToken {
				lastErr = fmt.Errorf("port is serving a different clubhouse")
				continue
			}
			return invite, nil
		}
	}
}

func readInvite() string {
	b, err := os.ReadFile(invitePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func tailFile(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	start := info.Size() - n
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	b, _ := io.ReadAll(f)
	return strings.TrimSpace(string(b))
}

func readPid() int {
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
