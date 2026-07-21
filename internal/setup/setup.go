// Package setup wires clubhouse into Codex: the PreToolUse/Stop hooks, the MCP
// server (auto-approved so it never prompts), and the /clubhouse skill. It edits
// ~/.codex only, is idempotent, and is fully reversible with Unsetup.
package setup

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clubhouse/internal/client"
	"clubhouse/internal/config"
)

const (
	begin = "# >>> clubhouse >>>"
	end   = "# <<< clubhouse <<<"
)

const promptFile = `---
description: Show who's in the clubhouse and the link to share
argument-hint: (no arguments)
---
Call the clubhouse ` + "`status`" + ` tool, then report clearly:
- who is currently in the clubhouse and what each teammate is doing
- the invite link to share with a teammate

Show the invite link verbatim. Then stop — take no other actions.
`

// skillFile is the 0.144+ way (custom prompts are deprecated). Skills are
// triggered by their description, so a plain "who's in the clubhouse?" fires it.
const skillFile = `---
name: clubhouse
description: Use whenever the user asks about the clubhouse — who is online, who is in the room, who is editing which file, the invite or share link, or wants to save or recall shared project memory. Triggers on "clubhouse", "who's here", "who's online", "share link".
metadata:
  short-description: Show clubhouse presence and the invite link
---
# Clubhouse

Clubhouse coordinates teammates running Codex in the same repo. Use these MCP tools:

- ` + "`status`" + ` — who is online and the invite link to share (use this for a plain "clubhouse" or "who's here" request)
- ` + "`presence`" + ` — who is in the room right now
- ` + "`whos_editing`" + ` — before editing a file, check whether a teammate holds it
- ` + "`lock`" + ` / ` + "`unlock`" + ` — claim or release a file
- ` + "`remember`" + ` / ` + "`recall`" + ` — shared team project memory

For "who's in the clubhouse" or the share link, call ` + "`status`" + `, show who is online and the invite link verbatim, then stop.
`

func codexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

type CheckResult struct {
	ConfigPath string
	Problems   []string
	Warnings   []string
	Fixed      bool
}

// Setup installs the hooks, MCP server, and clubhouse skill. Re-running updates
// the managed block in place.
func Setup() error {
	bin, ch, err := installAssets()
	if err != nil {
		return err
	}
	return rewriteConfig(ch, bin, false)
}

// Check verifies the Codex integration and optionally repairs duplicate or
// stale clubhouse-owned hook/MCP entries.
func Check(fix bool) (CheckResult, error) {
	bin, err := executablePath()
	if err != nil {
		return CheckResult{}, err
	}
	ch := codexHome()
	cfgPath := filepath.Join(ch, "config.toml")
	text := readFile(cfgPath)
	result := CheckResult{
		ConfigPath: cfgPath,
		Problems:   diagnose(text, bin),
		Warnings:   diagnoseRuntime(bin),
	}
	if fix {
		bin, ch, err := installAssets()
		if err != nil {
			return result, err
		}
		if err := rewriteConfig(ch, bin, true); err != nil {
			return result, err
		}
		result.Fixed = true
		result.Problems = diagnose(readFile(cfgPath), bin)
		result.Warnings = diagnoseRuntime(bin)
	}
	return result, nil
}

func executablePath() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", err
	}
	bin, _ = filepath.Abs(bin)
	return bin, nil
}

func installAssets() (bin, ch string, err error) {
	bin, err = executablePath()
	if err != nil {
		return "", "", err
	}
	ch = codexHome()

	if err := writeFile(filepath.Join(ch, "prompts", "clubhouse.md"), promptFile); err != nil {
		return "", "", err
	}
	if err := writeFile(filepath.Join(ch, "skills", "clubhouse", "SKILL.md"), skillFile); err != nil {
		return "", "", err
	}
	return bin, ch, nil
}

func rewriteConfig(ch, bin string, repairLegacy bool) error {
	// Clear any MCP entry a previous `codex mcp add` left outside our block,
	// so writing our own [mcp_servers.clubhouse] can't collide.
	exec.Command("codex", "mcp", "remove", "clubhouse").Run()

	cfgPath := filepath.Join(ch, "config.toml")
	text := stripBlock(readFile(cfgPath))
	if repairLegacy {
		text = stripMCPServer(text)
		text = stripLegacyHooks(text)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += managedBlock(bin)
	return writeFile(cfgPath, text)
}

// Unsetup removes everything Setup added.
func Unsetup() error {
	ch := codexHome()
	os.Remove(filepath.Join(ch, "prompts", "clubhouse.md"))
	os.RemoveAll(filepath.Join(ch, "skills", "clubhouse"))
	exec.Command("codex", "mcp", "remove", "clubhouse").Run()

	cfgPath := filepath.Join(ch, "config.toml")
	if text := readFile(cfgPath); text != "" {
		return writeFile(cfgPath, stripBlock(text))
	}
	return nil
}

// managedBlock registers the MCP server (auto-approved) + the hooks.
// ponytail: assumes the binary path has no spaces (codex splits the hook command
// string on spaces). True for a normal `go install`/homebrew path.
func managedBlock(bin string) string {
	return fmt.Sprintf(`%s
[mcp_servers.clubhouse]
command = "%s"
args = ["mcp"]
default_tools_approval_mode = "auto"

[[hooks.PreToolUse]]
matcher = ".*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "%s hook pre"

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "%s hook stop"
%s
`, begin, bin, bin, bin, end)
}

func stripBlock(text string) string {
	for {
		i := strings.Index(text, begin)
		j := strings.Index(text, end)
		if i < 0 || j < 0 || j < i {
			return strings.TrimLeft(text, "\n")
		}
		text = strings.TrimRight(text[:i], "\n") + "\n" + text[j+len(end):]
	}
}

func diagnose(text, bin string) []string {
	var problems []string
	if strings.Count(text, begin) > 1 || strings.Count(text, end) > 1 {
		problems = append(problems, "duplicate managed clubhouse config blocks")
	}
	if strings.Count(text, "clubhouse hook pre") > 1 {
		problems = append(problems, "duplicate PreToolUse clubhouse hooks")
	}
	if strings.Count(text, "clubhouse hook stop") > 1 {
		problems = append(problems, "duplicate Stop clubhouse hooks")
	}
	if strings.Count(text, "[mcp_servers.clubhouse]") > 1 {
		problems = append(problems, "duplicate clubhouse MCP server entries")
	}
	if !strings.Contains(text, "[mcp_servers.clubhouse]") {
		problems = append(problems, "missing clubhouse MCP server entry")
	}
	if !strings.Contains(text, "clubhouse hook pre") {
		problems = append(problems, "missing clubhouse PreToolUse hook")
	}
	if !strings.Contains(text, "clubhouse hook stop") {
		problems = append(problems, "missing clubhouse Stop hook")
	}
	if bin != "" && strings.Contains(text, "clubhouse") && !strings.Contains(text, bin) {
		problems = append(problems, "Codex config points at a different clubhouse binary")
	}
	for _, p := range configuredBinaryProblems(text) {
		problems = append(problems, p)
	}
	return problems
}

func diagnoseRuntime(bin string) []string {
	var warnings []string
	if _, err := exec.LookPath("codex"); err != nil {
		warnings = append(warnings, "codex is not on PATH; Codex integration cannot be used from this shell")
	}
	if path, err := exec.LookPath("clubhouse"); err != nil {
		warnings = append(warnings, "clubhouse is not on PATH; install it or add the install directory to PATH")
	} else if bin != "" {
		if abs, err := filepath.Abs(path); err == nil && filepath.Clean(abs) != filepath.Clean(bin) {
			warnings = append(warnings, "PATH resolves clubhouse to a different binary: "+abs)
		}
	}
	if w := daemonWarning(); w != "" {
		warnings = append(warnings, w)
	}
	if pids := clubhousePIDs("mcp"); len(pids) > 0 {
		warnings = append(warnings, "clubhouse mcp process already running: "+strings.Join(pids, ", "))
	}
	cfg, cfgErr := config.Load()
	token := config.ReadToken()
	if cfgErr != nil && token == "" {
		warnings = append(warnings, "not in a clubhouse yet; run `clubhouse host` or `clubhouse enter <invite>`")
		return warnings
	}
	if strings.TrimSpace(cfg.Server) == "" || token == "" {
		warnings = append(warnings, "local clubhouse server/token is missing; run `clubhouse host` or `clubhouse enter <invite>`")
		return warnings
	}
	hc := &http.Client{Timeout: 700 * time.Millisecond}
	if _, err := client.NewWithHTTPClient(cfg.Server, token, hc).Snapshot(); err != nil {
		warnings = append(warnings, "configured clubhouse server is unreachable: "+err.Error())
	}
	return warnings
}

func configuredBinaryProblems(text string) []string {
	seen := map[string]bool{}
	var problems []string
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, "command") || !strings.Contains(trim, "clubhouse") {
			continue
		}
		cmd, ok := tomlStringValue(trim)
		if !ok {
			continue
		}
		exe := firstField(cmd)
		if exe == "" || seen[exe] {
			continue
		}
		seen[exe] = true
		if strings.ContainsRune(exe, filepath.Separator) || filepath.IsAbs(exe) {
			if _, err := os.Stat(exe); err != nil {
				problems = append(problems, "configured clubhouse binary does not exist: "+exe)
			}
			continue
		}
		if _, err := exec.LookPath(exe); err != nil {
			problems = append(problems, "configured clubhouse binary is not on PATH: "+exe)
		}
	}
	return problems
}

func tomlStringValue(line string) (string, bool) {
	_, v, ok := strings.Cut(line, "=")
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return "", false
	}
	unq, err := strconv.Unquote(v)
	if err != nil {
		return "", false
	}
	return unq, true
}

func firstField(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func daemonWarning() string {
	b, err := os.ReadFile(filepath.Join(config.Dir, "serve.pid"))
	if err != nil {
		return ""
	}
	pid := strings.TrimSpace(string(b))
	if pid == "" {
		return ""
	}
	cmdline, err := exec.Command("ps", "-p", pid, "-o", "command=").Output()
	if err != nil || strings.TrimSpace(string(cmdline)) == "" {
		return "stale clubhouse daemon pid file: " + filepath.Join(config.Dir, "serve.pid")
	}
	if !strings.Contains(string(cmdline), "clubhouse __serve") {
		return "clubhouse daemon pid belongs to another process: " + pid
	}
	return ""
}

func clubhousePIDs(mode string) []string {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	var pids []string
	needle := "clubhouse " + mode
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, needle) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			pids = append(pids, fields[0])
		}
	}
	return pids
}

func stripMCPServer(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[mcp_servers.clubhouse") {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(trim, "[") {
			skip = false
		}
		if !skip {
			out = append(out, line)
		}
	}
	return compactBlankLines(strings.Join(out, "\n"))
}

func stripLegacyHooks(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		trim := strings.TrimSpace(lines[i])
		if trim == "[[hooks.PreToolUse]]" || trim == "[[hooks.Stop]]" {
			j := i + 1
			for j < len(lines) {
				next := strings.TrimSpace(lines[j])
				if next == "[[hooks.PreToolUse]]" || next == "[[hooks.Stop]]" || (strings.HasPrefix(next, "[") && !strings.Contains(next, ".hooks")) {
					break
				}
				j++
			}
			block := strings.Join(lines[i:j], "\n")
			if strings.Contains(block, "clubhouse hook pre") || strings.Contains(block, "clubhouse hook stop") {
				i = j
				continue
			}
		}
		out = append(out, lines[i])
		i++
	}
	return compactBlankLines(strings.Join(out, "\n"))
}

func compactBlankLines(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}
