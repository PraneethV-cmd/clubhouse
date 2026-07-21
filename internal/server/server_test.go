package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clubhouse/internal/client"
	"clubhouse/internal/room"
	"clubhouse/internal/server"
)

func TestRedeemJoinLock(t *testing.T) {
	s := server.New("test", "roomtok", "", "http://box:8787")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// The current join code comes from the invite endpoint.
	link, err := client.New(ts.URL, "roomtok").Invite()
	if err != nil {
		t.Fatal(err)
	}
	_, code, err := room.ParseInvite(link)
	if err != nil {
		t.Fatal(err)
	}

	// Right code redeems the room token; a bogus code is rejected.
	tok, err := client.Redeem(ts.URL, code)
	if err != nil || tok != "roomtok" {
		t.Fatalf("redeem: tok=%q err=%v", tok, err)
	}
	if _, err := client.Redeem(ts.URL, "bogus"); err == nil {
		t.Fatal("expected bogus code to be rejected")
	}

	// Two members, and the collision block still works.
	alice, bob := client.New(ts.URL, tok), client.New(ts.URL, tok)
	if _, err := alice.Join("ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.Join("bo"); err != nil {
		t.Fatal(err)
	}
	if _, err := alice.Lock("auth.go", "refactor"); err != nil {
		t.Fatal(err)
	}
	var conflict client.LockConflict
	if _, err := bob.Lock("auth.go", "steal"); !errors.As(err, &conflict) {
		t.Fatalf("expected LockConflict, got %v", err)
	}
}

func TestBlockedEventIsPersisted(t *testing.T) {
	snap := filepath.Join(t.TempDir(), ".clubhouse", "room.json")
	s := server.New("test", "roomtok", snap, "http://box:8787")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	ada := client.New(ts.URL, "roomtok")
	if _, err := ada.Join("ada"); err != nil {
		t.Fatal(err)
	}
	if err := ada.ReportBlocked("auth.go"); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	var saved room.Room
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Events) == 0 || saved.Events[len(saved.Events)-1].Kind != "blocked" {
		t.Fatalf("blocked event was not persisted: %#v", saved.Events)
	}
}

func TestDecodeRejectsLargeBodies(t *testing.T) {
	s := server.New("test", "roomtok", "", "http://box:8787")
	body := `{"code":"` + strings.Repeat("x", 1<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/redeem", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("large request body should not be accepted")
	}
}

func TestSweepReleasesLocksWhenMemberLeaves(t *testing.T) {
	server.SetLeaseDurations(15*time.Millisecond, time.Minute, time.Hour)
	t.Cleanup(func() { server.SetLeaseDurations(60*time.Second, 2*time.Minute, 10*time.Second) })

	s := server.New("test", "roomtok", "", "http://box:8787")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	ada := client.New(ts.URL, "roomtok")
	if _, err := ada.Join("ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := ada.Lock("auth.go", "editing"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(25 * time.Millisecond)
	s.SweepOnce()

	r, err := ada.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Locks) != 0 {
		t.Fatalf("locks should be released when member leaves: %#v", r.Locks)
	}
	if len(r.Members) != 0 {
		t.Fatalf("stale member should be pruned: %#v", r.Members)
	}
	if len(r.Events) == 0 || !strings.Contains(r.Events[len(r.Events)-1].Detail, "member left") {
		t.Fatalf("release event should explain member left: %#v", r.Events)
	}
}

func TestUnknownMemberCannotClaimLock(t *testing.T) {
	s := server.New("test", "roomtok", "", "http://box:8787")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	stale := client.New(ts.URL, "roomtok")
	stale.SetID("missing-member")
	if _, err := stale.Lock("auth.go", "editing"); err == nil {
		t.Fatal("stale member should not be able to claim a lock")
	}

	r, err := client.New(ts.URL, "roomtok").Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Locks) != 0 {
		t.Fatalf("stale member created locks: %#v", r.Locks)
	}
}

func TestSweepReleasesOrphanLocksFromSnapshot(t *testing.T) {
	snap := filepath.Join(t.TempDir(), ".clubhouse", "room.json")
	if err := os.MkdirAll(filepath.Dir(snap), 0o755); err != nil {
		t.Fatal(err)
	}
	old := room.Room{
		Name:    "test",
		Members: map[string]room.Member{},
		Locks: map[string]room.Lock{
			"auth.go": {Path: "auth.go", Member: "gone", Reason: "editing", Since: time.Now()},
		},
	}
	b, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap, b, 0o644); err != nil {
		t.Fatal(err)
	}

	s := server.New("test", "roomtok", snap, "http://box:8787")
	s.SweepOnce()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	r, err := client.New(ts.URL, "roomtok").Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Locks) != 0 {
		t.Fatalf("orphan lock should have been released: %#v", r.Locks)
	}
	if len(r.Events) == 0 || !strings.Contains(r.Events[len(r.Events)-1].Detail, "orphaned") {
		t.Fatalf("release event should explain orphan cleanup: %#v", r.Events)
	}
}
