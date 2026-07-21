// Package mcp exposes the clubhouse to a Codex agent as MCP tools, so the agent
// itself becomes room-aware: it can see who's around, check who's editing a
// file, claim/release files, and read or write shared project memory.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"clubhouse/internal/client"
	"clubhouse/internal/room"
	clubsession "clubhouse/internal/session"
)

type pathArgs struct {
	Path   string `json:"path" jsonschema:"repo-relative file path"`
	Reason string `json:"reason,omitempty" jsonschema:"why you're claiming it"`
}
type textArgs struct {
	Text string `json:"text" jsonschema:"the note to remember for the team"`
}
type noArgs struct{}

type clientProvider func() (*client.Client, error)

// Run joins the clubhouse for this Codex session and serves the tools over stdio
// until Codex disconnects.
func Run() error {
	get := liveClient
	srv := mcp.NewServer(&mcp.Implementation{Name: "clubhouse", Version: "0.1.0"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "status", Description: "Show who's in the clubhouse and the invite link to share with a teammate."}, status(get))
	mcp.AddTool(srv, &mcp.Tool{Name: "presence", Description: "Who is in the clubhouse right now."}, presence(get))
	mcp.AddTool(srv, &mcp.Tool{Name: "whos_editing", Description: "Check whether a file is being edited, and by whom, before you touch it."}, whosEditing(get))
	mcp.AddTool(srv, &mcp.Tool{Name: "lock", Description: "Claim a file so teammates' agents can't edit it until you release it."}, lock(get))
	mcp.AddTool(srv, &mcp.Tool{Name: "unlock", Description: "Release a file you claimed."}, unlock(get))
	mcp.AddTool(srv, &mcp.Tool{Name: "remember", Description: "Save a note to shared project memory for the whole team."}, remember(get))
	mcp.AddTool(srv, &mcp.Tool{Name: "recall", Description: "Read the team's shared project memory — what's been tried and what to know."}, recall(get))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go heartbeat(ctx, get)
	return srv.Run(ctx, &mcp.StdioTransport{})
}

func liveClient() (*client.Client, error) {
	c, _, err := clubsession.Client("running Codex")
	return c, err
}

// heartbeat keeps this member visible in the room while Codex is running.
func heartbeat(ctx context.Context, get clientProvider) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		if c, err := get(); err == nil {
			c.Heartbeat("running Codex")
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func status(get clientProvider) mcp.ToolHandlerFor[noArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		c, err := get()
		if err != nil {
			return text(notInClubhouse(err)), nil, nil
		}
		r, err := c.Snapshot()
		if err != nil {
			return text("clubhouse unreachable"), nil, nil
		}
		invite, _ := c.Invite()
		var b strings.Builder
		b.WriteString("🏠 clubhouse: " + orDash(r.Name) + "\n\n")
		var online []string
		for _, m := range r.Members {
			if time.Since(m.LastSeen) < 10*time.Second {
				s := "• " + m.Name
				if m.Status != "" {
					s += " — " + m.Status
				}
				if m.Git.Summary != "" {
					s += " — git: " + m.Git.Summary
				}
				online = append(online, s)
			}
		}
		sort.Strings(online)
		if len(online) == 0 {
			b.WriteString("nobody else is here right now\n")
		} else {
			b.WriteString("in the clubhouse:\n" + strings.Join(online, "\n") + "\n")
		}
		b.WriteString("\ninvite a teammate:\n" + invite + "\n")
		return text(b.String()), nil, nil
	}
}

func notInClubhouse(err error) string {
	if err == nil {
		return "not in a clubhouse - run: clubhouse enter rc://join/..."
	}
	return "not in a clubhouse - run: clubhouse enter rc://join/... (" + err.Error() + ")"
}

func orDash(s string) string {
	if s == "" {
		return "clubhouse"
	}
	return s
}

func presence(get clientProvider) mcp.ToolHandlerFor[noArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		c, err := get()
		if err != nil {
			return text(notInClubhouse(err)), nil, nil
		}
		r, err := c.Snapshot()
		if err != nil {
			return text("clubhouse unreachable"), nil, nil
		}
		var online []string
		for _, m := range r.Members {
			if time.Since(m.LastSeen) < 10*time.Second {
				s := m.Name
				if m.Status != "" {
					s += " (" + m.Status + ")"
				}
				if m.Git.Summary != "" {
					s += " [" + m.Git.Summary + "]"
				}
				online = append(online, s)
			}
		}
		sort.Strings(online)
		if len(online) == 0 {
			return text("nobody else is here right now"), nil, nil
		}
		return text("in the clubhouse: " + strings.Join(online, ", ")), nil, nil
	}
}

func whosEditing(get clientProvider) mcp.ToolHandlerFor[pathArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in pathArgs) (*mcp.CallToolResult, any, error) {
		c, err := get()
		if err != nil {
			return text(notInClubhouse(err)), nil, nil
		}
		in.Path = cleanPath(in.Path)
		r, err := c.Snapshot()
		if err != nil {
			return text("clubhouse unreachable"), nil, nil
		}
		if l, ok := r.Locks[in.Path]; ok {
			return text(fmt.Sprintf("%s is being edited by %s — %s", in.Path, memberName(r, l.Member), l.Reason)), nil, nil
		}
		return text(in.Path + " is free"), nil, nil
	}
}

func lock(get clientProvider) mcp.ToolHandlerFor[pathArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in pathArgs) (*mcp.CallToolResult, any, error) {
		c, err := get()
		if err != nil {
			return text(notInClubhouse(err)), nil, nil
		}
		in.Path = cleanPath(in.Path)
		reason := in.Reason
		if reason == "" {
			reason = "editing"
		}
		if _, err := c.Lock(in.Path, reason); err != nil {
			var conflict client.LockConflict
			if errors.As(err, &conflict) {
				return text(fmt.Sprintf("can't claim %s — already held: %s", in.Path, conflict.Held.Reason)), nil, nil
			}
			return text("couldn't claim: " + err.Error()), nil, nil
		}
		return text("claimed " + in.Path), nil, nil
	}
}

func unlock(get clientProvider) mcp.ToolHandlerFor[pathArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in pathArgs) (*mcp.CallToolResult, any, error) {
		c, err := get()
		if err != nil {
			return text(notInClubhouse(err)), nil, nil
		}
		in.Path = cleanPath(in.Path)
		if _, err := c.Unlock(in.Path); err != nil {
			return text("couldn't release: " + err.Error()), nil, nil
		}
		return text("released " + in.Path), nil, nil
	}
}

func remember(get clientProvider) mcp.ToolHandlerFor[textArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in textArgs) (*mcp.CallToolResult, any, error) {
		c, err := get()
		if err != nil {
			return text(notInClubhouse(err)), nil, nil
		}
		if _, err := c.Memory(in.Text); err != nil {
			return text("couldn't save: " + err.Error()), nil, nil
		}
		return text("saved to project memory"), nil, nil
	}
}

func recall(get clientProvider) mcp.ToolHandlerFor[noArgs, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		c, err := get()
		if err != nil {
			return text(notInClubhouse(err)), nil, nil
		}
		r, err := c.Snapshot()
		if err != nil {
			return text("clubhouse unreachable"), nil, nil
		}
		if len(r.Notes) == 0 {
			return text("no project memory yet"), nil, nil
		}
		var b strings.Builder
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "- %s: %s\n", n.Author, n.Text)
		}
		return text(b.String()), nil, nil
	}
}

func memberName(r room.Room, id string) string {
	if m, ok := r.Members[id]; ok && m.Name != "" {
		return m.Name
	}
	return "a teammate"
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, p); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
				return filepath.ToSlash(rel)
			}
		}
	}
	p = filepath.Clean(p)
	if p == "." {
		return ""
	}
	return filepath.ToSlash(p)
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}
