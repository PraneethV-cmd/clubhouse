package dash

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"clubhouse/internal/room"
)

func TestViewRenders(t *testing.T) {
	now := time.Now()
	r := room.Room{
		Name: "payments-api",
		Members: map[string]room.Member{
			"1": {ID: "1", Name: "turbo-otter", Status: "refactoring auth", Git: room.GitStatus{Summary: "main | 2 changed"}, LastSeen: now},
			"2": {ID: "2", Name: "sleepy-panda", Status: "fixing billing", Git: room.GitStatus{Summary: "feature/billing | clean | ahead 1"}, LastSeen: now},
			"3": {ID: "3", Name: "swift-gecko", LastSeen: now.Add(-time.Minute)}, // offline
		},
		Locks: map[string]room.Lock{
			"src/auth.ts":    {Path: "src/auth.ts", Member: "1", Reason: "editing"},
			"src/billing.ts": {Path: "src/billing.ts", Member: "2", Reason: "editing"},
		},
		Notes: []room.Note{{Author: "turbo-otter", Text: "webhooks idempotent"}},
		Events: []room.Event{
			{At: now, Kind: "joined", Actor: "turbo-otter"},
			{At: now, Kind: "locked", Actor: "turbo-otter", Detail: "src/auth.ts"},
			{At: now, Kind: "blocked", Actor: "sleepy-panda", Detail: "src/auth.ts"},
		},
	}

	m := newModel(nil)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	mm, _ = mm.Update(dataMsg{room: r, invite: "rc://join/abc123"})
	out := mm.View()

	for _, want := range []string{"clubhouse", "PEOPLE", "LOCKS", "EVENTS", "MEMORY", "payments-api", "2 online", "turbo-otter", "main | 2 changed", "src/auth.ts", "webhooks", "was blocked on", "rc://join/abc123"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	t.Log("\n" + out)
}

func TestViewRendersAtMinimumSize(t *testing.T) {
	m := newModel(nil)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: minWindowWidth, Height: minWindowHeight})
	out := mm.View()
	if strings.Contains(out, "minimum window") {
		t.Fatalf("minimum size should render the lounge, got:\n%s", out)
	}
	for _, want := range []string{"clubhouse", "PEOPLE", "LOCKS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("minimum-size view missing %q:\n%s", want, out)
		}
	}
}

func TestViewShowsMinimumSizeMessage(t *testing.T) {
	m := newModel(nil)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	out := mm.View()
	for _, want := range []string{"clubhouse lounge", "minimum window: 80x24", "current window: 70x20"} {
		if !strings.Contains(out, want) {
			t.Fatalf("small-window view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "PEOPLE") || strings.Contains(out, "LOCKS") {
		t.Fatalf("small-window view should not render dense dashboard:\n%s", out)
	}
}
