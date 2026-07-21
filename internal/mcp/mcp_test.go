package mcp

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"clubhouse/internal/client"
	"clubhouse/internal/server"
)

func TestToolsSmoke(t *testing.T) {
	s := server.New("test", "roomtok", "", "http://box:8787")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	c := client.New(ts.URL, "roomtok")
	if _, err := c.Join("ada"); err != nil {
		t.Fatal(err)
	}
	get := func() (*client.Client, error) { return c, nil }

	got := callNoArgs(t, presence(get))
	if !strings.Contains(got, "ada") {
		t.Fatalf("presence = %q", got)
	}

	got = callPath(t, lock(get), pathArgs{Path: "src/auth.go", Reason: "editing auth"})
	if !strings.Contains(got, "claimed src/auth.go") {
		t.Fatalf("lock = %q", got)
	}

	got = callPath(t, whosEditing(get), pathArgs{Path: "src/auth.go"})
	if !strings.Contains(got, "ada") || !strings.Contains(got, "editing auth") {
		t.Fatalf("whos_editing = %q", got)
	}

	got = callText(t, remember(get), textArgs{Text: "release checklist lives in PRODUCTION_CHECKLIST.md"})
	if !strings.Contains(got, "saved") {
		t.Fatalf("remember = %q", got)
	}

	got = callNoArgs(t, recall(get))
	if !strings.Contains(got, "release checklist") {
		t.Fatalf("recall = %q", got)
	}

	got = callPath(t, unlock(get), pathArgs{Path: "src/auth.go"})
	if !strings.Contains(got, "released src/auth.go") {
		t.Fatalf("unlock = %q", got)
	}
}

func TestToolsReturnGuidanceWhenNotInClubhouse(t *testing.T) {
	get := func() (*client.Client, error) { return nil, errors.New("not in a clubhouse") }
	got := callNoArgs(t, status(get))
	if !strings.Contains(got, "clubhouse enter rc://join/") {
		t.Fatalf("status guidance = %q", got)
	}
	got = callPath(t, lock(get), pathArgs{Path: "src/auth.go"})
	if !strings.Contains(got, "clubhouse enter rc://join/") {
		t.Fatalf("lock guidance = %q", got)
	}
}

func callNoArgs(t *testing.T, h mcpsdk.ToolHandlerFor[noArgs, any]) string {
	t.Helper()
	res, _, err := h(context.Background(), &mcpsdk.CallToolRequest{}, noArgs{})
	if err != nil {
		t.Fatal(err)
	}
	return resultText(t, res)
}

func callPath(t *testing.T, h mcpsdk.ToolHandlerFor[pathArgs, any], in pathArgs) string {
	t.Helper()
	res, _, err := h(context.Background(), &mcpsdk.CallToolRequest{}, in)
	if err != nil {
		t.Fatal(err)
	}
	return resultText(t, res)
}

func callText(t *testing.T, h mcpsdk.ToolHandlerFor[textArgs, any], in textArgs) string {
	t.Helper()
	res, _, err := h(context.Background(), &mcpsdk.CallToolRequest{}, in)
	if err != nil {
		t.Fatal(err)
	}
	return resultText(t, res)
}

func resultText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	txt, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("unexpected content type %T", res.Content[0])
	}
	return txt.Text
}
