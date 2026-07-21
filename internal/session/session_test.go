package session

import (
	"net/http/httptest"
	"testing"

	"clubhouse/internal/client"
	"clubhouse/internal/config"
	"clubhouse/internal/server"
)

func TestJoinOrResumeRejoinsStaleSession(t *testing.T) {
	t.Chdir(t.TempDir())

	s := server.New("test", "roomtok", "", "http://box:8787")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	cfg := config.Config{Name: "ada", Server: ts.URL}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteToken("roomtok"); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteSession("stale-member"); err != nil {
		t.Fatal(err)
	}

	c := client.New(ts.URL, "roomtok")
	r, err := JoinOrResume(c, "ada", "testing")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID() == "" || c.ID() == "stale-member" {
		t.Fatalf("expected a fresh member id, got %q", c.ID())
	}
	if _, ok := r.Members[c.ID()]; !ok {
		t.Fatalf("fresh member missing from room: %v", r.Members)
	}
	if got := config.ReadSession(); got != c.ID() {
		t.Fatalf("session file = %q, want %q", got, c.ID())
	}
}
