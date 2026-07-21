// Package room holds the shared state of a clubhouse and the wire types used
// between the coordinator and its clients.
package room

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Room is the entire shared state of one clubhouse. It lives in the coordinator
// and is handed to clients whole on every request.
type Room struct {
	Name    string            `json:"name"`
	Members map[string]Member `json:"members"`
	Locks   map[string]Lock   `json:"locks"` // keyed by file path
	Notes   []Note            `json:"notes"`
	Events  []Event           `json:"events"` // recent activity feed (bounded)
}

// Event is one line in the live activity feed.
type Event struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // joined | locked | released | blocked
	Actor  string    `json:"actor"`
	Detail string    `json:"detail"`
}

type Member struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Status   string    `json:"status"` // what their agent is doing right now
	Git      GitStatus `json:"git,omitempty"`
	LastSeen time.Time `json:"last_seen"`
}

type GitStatus struct {
	Branch  string    `json:"branch,omitempty"`
	Dirty   int       `json:"dirty,omitempty"`
	Ahead   int       `json:"ahead,omitempty"`
	Behind  int       `json:"behind,omitempty"`
	Summary string    `json:"summary,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

type Lock struct {
	Path   string    `json:"path"`
	Member string    `json:"member"` // member ID holding it
	Reason string    `json:"reason"`
	Since  time.Time `json:"since"`
}

type Note struct {
	Author string    `json:"author"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}

// Wire types shared by server and client.
//
// Two secrets: a rotating join Code (in the invite link, redeemed once) and a
// stable room Token (bearer for every authenticated call). Rotating the code
// expires old links without kicking out members who already hold the token.
type (
	RedeemReq  struct{ Code string }
	RedeemResp struct{ Token string }
	JoinReq    struct{ Name string }
	JoinResp   struct {
		MemberID string
		Room     Room
	}
	BeatReq struct {
		MemberID string
		Status   string
		Git      GitStatus
	}
	LockReq    struct{ MemberID, Path, Reason string }
	NoteReq    struct{ MemberID, Text string }
	InviteResp struct{ Link string }
)

// NewID returns a short random hex id for members, tokens, and join codes.
func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Invite links: rc://join/<base64(server|code)>.
func MakeInvite(server, code string) string {
	return "rc://join/" + base64.RawURLEncoding.EncodeToString([]byte(server+"|"+code))
}

func ParseInvite(link string) (server, code string, err error) {
	enc := strings.TrimPrefix(strings.TrimSpace(link), "rc://join/")
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", "", fmt.Errorf("bad invite link")
	}
	s, c, ok := strings.Cut(string(b), "|")
	if !ok {
		return "", "", fmt.Errorf("bad invite link")
	}
	return s, c, nil
}
