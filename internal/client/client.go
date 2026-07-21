// Package client talks to a clubhouse coordinator on behalf of one member.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"clubhouse/internal/room"
)

const defaultTimeout = 2 * time.Second

type Client struct {
	server string
	token  string
	id     string
	hc     *http.Client
}

func New(server, token string) *Client {
	return &Client{server: strings.TrimRight(server, "/"), token: token, hc: &http.Client{Timeout: defaultTimeout}}
}

func NewWithHTTPClient(server, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{server: strings.TrimRight(server, "/"), token: token, hc: hc}
}

// ID / SetID let a short-lived process (a hook) reuse a member id across runs.
func (c *Client) ID() string      { return c.id }
func (c *Client) SetID(id string) { c.id = id }

// Redeem exchanges a (rotating) join code for the stable room token.
func Redeem(server, code string) (string, error) {
	c := New(server, "")
	var resp room.RedeemResp
	if err := c.post("/redeem", room.RedeemReq{Code: code}, &resp); err != nil {
		return "", err
	}
	return resp.Token, nil
}

// Join registers this member and remembers the returned ID for later calls.
func (c *Client) Join(name string) (room.Room, error) {
	var resp room.JoinResp
	if err := c.post("/join", room.JoinReq{Name: name}, &resp); err != nil {
		return room.Room{}, err
	}
	c.id = resp.MemberID
	return resp.Room, nil
}

func (c *Client) Heartbeat(status string) (room.Room, error) {
	var r room.Room
	err := c.post("/heartbeat", room.BeatReq{MemberID: c.id, Status: status, Git: gitStatus()}, &r)
	return r, err
}

// Lock returns a LockConflict if another member holds the path.
func (c *Client) Lock(path, reason string) (room.Room, error) {
	var r room.Room
	err := c.post("/lock", room.LockReq{MemberID: c.id, Path: path, Reason: reason}, &r)
	return r, err
}

func (c *Client) Unlock(path string) (room.Room, error) {
	var r room.Room
	err := c.post("/unlock", room.LockReq{MemberID: c.id, Path: path}, &r)
	return r, err
}

// ReportBlocked records a client-side denial in the activity feed.
func (c *Client) ReportBlocked(path string) error {
	var out struct{}
	return c.post("/blocked", room.LockReq{MemberID: c.id, Path: path}, &out)
}

func (c *Client) Memory(text string) (room.Room, error) {
	var r room.Room
	err := c.post("/memory", room.NoteReq{MemberID: c.id, Text: text}, &r)
	return r, err
}

// Snapshot reads the room without changing anything.
func (c *Client) Snapshot() (room.Room, error) {
	var r room.Room
	err := c.get("/room", &r)
	return r, err
}

// Invite returns the current (rotating) invite link from the coordinator.
func (c *Client) Invite() (string, error) {
	var resp room.InviteResp
	err := c.get("/invite", &resp)
	return resp.Link, err
}

// LockConflict is returned by Lock when another member holds the file.
type LockConflict struct{ Held room.Lock }

func (e LockConflict) Error() string {
	return fmt.Sprintf("%s is locked by another agent: %s", e.Held.Path, e.Held.Reason)
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.server+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) post(path string, req, out any) error {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.server+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		var held room.Lock
		json.NewDecoder(resp.Body).Decode(&held)
		return LockConflict{Held: held}
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

var aheadBehindRE = regexp.MustCompile(`\[(?:ahead ([0-9]+))?(?:, )?(?:behind ([0-9]+))?\]`)

func gitStatus() room.GitStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "status", "--short", "--branch")
	out, err := cmd.Output()
	if err != nil {
		return room.GitStatus{}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return room.GitStatus{}
	}
	g := room.GitStatus{At: time.Now()}
	head := strings.TrimPrefix(strings.TrimSpace(lines[0]), "## ")
	if before, _, ok := strings.Cut(head, "..."); ok {
		g.Branch = strings.TrimSpace(before)
	} else {
		g.Branch = strings.TrimSpace(strings.TrimPrefix(head, "HEAD detached at "))
	}
	if m := aheadBehindRE.FindStringSubmatch(head); len(m) == 3 {
		g.Ahead, _ = strconv.Atoi(m[1])
		g.Behind, _ = strconv.Atoi(m[2])
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			g.Dirty++
		}
	}
	g.Summary = gitSummary(g)
	return g
}

func gitSummary(g room.GitStatus) string {
	var parts []string
	if g.Branch != "" {
		parts = append(parts, g.Branch)
	}
	if g.Dirty == 0 {
		parts = append(parts, "clean")
	} else {
		parts = append(parts, fmt.Sprintf("%d changed", g.Dirty))
	}
	if g.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("ahead %d", g.Ahead))
	}
	if g.Behind > 0 {
		parts = append(parts, fmt.Sprintf("behind %d", g.Behind))
	}
	return strings.Join(parts, " | ")
}
