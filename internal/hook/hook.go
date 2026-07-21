// Package hook implements the Codex PreToolUse/Stop hooks that hard-enforce
// clubhouse file locks. On a file edit, the first toucher auto-claims the lock;
// everyone else is denied until it's released.
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"clubhouse/internal/client"
	"clubhouse/internal/room"
	clubsession "clubhouse/internal/session"
)

// input is the JSON Codex sends a hook on stdin. Codex edits files via the
// apply_patch tool, which carries the paths inside tool_input.command as a
// patch; Write/Edit (other agents) carry a plain file_path.
type input struct {
	ToolName  string `json:"tool_name"`
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Command  string `json:"command"` // apply_patch: the patch text
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	} `json:"tool_input"`
}

// files returns every repo-relative path this edit would touch.
func (in input) files() []string {
	var raw []string
	if in.ToolInput.Command != "" {
		raw = append(raw, patchPaths(in.ToolInput.Command)...)
	}
	if p := firstNonEmpty(in.ToolInput.FilePath, in.ToolInput.Path); p != "" {
		raw = append(raw, p)
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range raw {
		rel := in.rel(p)
		if rel != "" && !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	return out
}

// rel keys locks on the repo-relative path so they match across checkouts.
func (in input) rel(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/dev/null" {
		return ""
	}
	cwd := in.Cwd
	if cwd != "" {
		cwd = filepath.Clean(cwd)
	}
	if filepath.IsAbs(p) {
		p = filepath.Clean(p)
		if cwd == "" {
			return ""
		}
		r, err := filepath.Rel(cwd, p)
		if err != nil || r == "." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || r == ".." {
			return ""
		}
		return filepath.ToSlash(r)
	}
	p = filepath.Clean(p)
	if p == "." || strings.HasPrefix(p, ".."+string(filepath.Separator)) || p == ".." {
		return ""
	}
	return filepath.ToSlash(p)
}

// patchPaths pulls file paths out of an apply_patch body:
// `*** Add File: x`, `*** Update File: x`, `*** Delete File: x`, `*** Move to: x`.
func patchPaths(patch string) []string {
	var out []string
	for _, line := range strings.Split(patch, "\n") {
		for _, pre := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if strings.HasPrefix(line, pre) {
				out = append(out, strings.TrimSpace(strings.TrimPrefix(line, pre)))
			}
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Pre handles PreToolUse: claim the file or deny if a teammate holds it.
// Fails OPEN on any coordinator trouble — the coordination layer must never
// brick real work.
func Pre() {
	var in input
	json.NewDecoder(os.Stdin).Decode(&in)
	// Codex may invoke the hook from a different cwd, so anchor to the repo it
	// reports before looking up .clubhouse/ config and session.
	if in.Cwd != "" {
		if err := os.Chdir(in.Cwd); err != nil {
			allow()
			return
		}
	}
	c, err := session()
	if err != nil {
		allow() // not in a clubhouse: fail open, never brick real work
		return
	}

	// File edits (apply_patch / Write / Edit): claim each path or deny.
	if files := in.files(); len(files) > 0 {
		reason, blocked := claimFiles(c, files)
		if blocked {
			deny(reason)
			return
		}
		allow()
		return
	}

	// Shell commands can edit too (sed -i, cat >). Block a write that touches a
	// file a teammate holds.
	if in.ToolName == "Bash" && in.ToolInput.Command != "" {
		if reason := bashConflict(c, in.ToolInput.Command); reason != "" {
			deny(reason)
			return
		}
	}
	allow()
}

// bashConflict returns a deny reason if cmd looks like a write and mentions a
// path another member holds. Heuristic, but closes the shell-edit bypass.
func bashConflict(c *client.Client, cmd string) string {
	if !looksLikeWrite(cmd) {
		return ""
	}
	r, err := c.Snapshot()
	if err != nil {
		return "" // fail open
	}
	for _, l := range r.Locks {
		if l.Member != c.ID() && strings.Contains(cmd, l.Path) {
			name := "a teammate"
			if m, ok := r.Members[l.Member]; ok && m.Name != "" {
				name = m.Name
			}
			c.ReportBlocked(l.Path) // show it in the live feed
			return fmt.Sprintf("that command writes %s, which %s is editing — wait or pick another file", l.Path, name)
		}
	}
	return ""
}

func looksLikeWrite(cmd string) bool {
	for _, w := range []string{">", ">>", "tee ", "sed -i", " -i ", "dd ", "truncate", "mv ", "cp ", "rm ", "chmod ", "chown ", "patch "} {
		if strings.Contains(cmd, w) {
			return true
		}
	}
	return false
}

func claimFiles(c *client.Client, files []string) (reason string, blocked bool) {
	var claimed []string
	for _, path := range files {
		if _, err := c.Lock(path, "editing"); err != nil {
			for _, locked := range claimed {
				c.Unlock(locked)
			}
			var conflict client.LockConflict
			if errors.As(err, &conflict) {
				return fmt.Sprintf("%s is being edited by %s — wait or pick another file", path, holderName(c, conflict.Held)), true
			}
			return "", false // coordinator down: fail open
		}
		claimed = append(claimed, path)
	}
	return "", false
}

// Stop releases every lock this member holds when the Codex session ends.
func Stop() {
	var in input
	json.NewDecoder(os.Stdin).Decode(&in)
	if in.Cwd != "" {
		if err := os.Chdir(in.Cwd); err != nil {
			return
		}
	}
	c, err := session()
	if err != nil {
		return
	}
	r, err := c.Snapshot()
	if err != nil {
		return
	}
	for path, l := range r.Locks {
		if l.Member == c.ID() {
			c.Unlock(path)
		}
	}
}

// session returns a client bound to this member, joining once and caching the id.
func session() (*client.Client, error) {
	c, _, err := clubsession.Client("running Codex")
	return c, err
}

func holderName(c *client.Client, l room.Lock) string {
	if r, err := c.Snapshot(); err == nil {
		if m, ok := r.Members[l.Member]; ok && m.Name != "" {
			return m.Name
		}
	}
	return "a teammate"
}

func allow() {
	// Codex treats successful hooks as allow-by-default. Returning
	// permissionDecision:"allow" is rejected by current Codex versions.
}

// deny blocks the edit two ways for robustness: the JSON permission decision
// (honored in normal permission modes) and exit code 2 with the reason on
// stderr (honored even when permissions are bypassed).
func deny(reason string) {
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	})
	fmt.Println(string(out))
	fmt.Fprintln(os.Stderr, reason)
	notify(reason)
	os.Exit(2)
}

// notify pops a native macOS notification (best-effort; no-op elsewhere).
func notify(reason string) {
	script := fmt.Sprintf(`display notification %q with title "🏠 clubhouse" subtitle "edit blocked"`, reason)
	exec.Command("osascript", "-e", script).Run()
}
