package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripBlockRemovesAllManagedBlocks(t *testing.T) {
	text := "before\n" + managedBlock("/bin/clubhouse") + "\nbetween\n" + managedBlock("/bin/clubhouse") + "\nafter\n"
	got := stripBlock(text)
	if strings.Contains(got, begin) || strings.Contains(got, end) {
		t.Fatalf("managed block markers remain:\n%s", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "between") || !strings.Contains(got, "after") {
		t.Fatalf("unmanaged text was removed:\n%s", got)
	}
}

func TestStripLegacyClubhouseSections(t *testing.T) {
	text := `
[mcp_servers.other]
command = "other"

[mcp_servers.clubhouse]
command = "/old/clubhouse"
args = ["mcp"]
[mcp_servers.clubhouse.tools.presence]
default = true

[[hooks.PreToolUse]]
matcher = ".*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "/old/clubhouse hook pre"

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "/old/clubhouse hook stop"

[[hooks.PreToolUse]]
matcher = "keep"
`
	got := stripLegacyHooks(stripMCPServer(text))
	for _, bad := range []string{"mcp_servers.clubhouse", "clubhouse hook pre", "clubhouse hook stop"} {
		if strings.Contains(got, bad) {
			t.Fatalf("legacy clubhouse config remains (%s):\n%s", bad, got)
		}
	}
	for _, want := range []string{"mcp_servers.other", `matcher = "keep"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("wanted config missing (%s):\n%s", want, got)
		}
	}
}

func TestDiagnoseConfiguredBinaryDoesNotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-clubhouse")
	got := diagnose(managedBlock(missing), missing)
	if !hasProblem(got, "configured clubhouse binary does not exist") {
		t.Fatalf("missing binary was not diagnosed: %v", got)
	}
}

func TestDiagnoseConfiguredBinaryExists(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "clubhouse")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := diagnose(managedBlock(bin), bin)
	if hasProblem(got, "configured clubhouse binary does not exist") {
		t.Fatalf("existing binary was diagnosed as missing: %v", got)
	}
}

func hasProblem(problems []string, sub string) bool {
	for _, p := range problems {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}
