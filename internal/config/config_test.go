package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRoomSeparatesMemberAndClubhouseName(t *testing.T) {
	cfg := Parse(`
name = ada
room = launch room
server = http://127.0.0.1:8787
addr = 127.0.0.1:9999
`)
	if cfg.Name != "ada" {
		t.Fatalf("Name = %q, want ada", cfg.Name)
	}
	if cfg.Room != "launch room" {
		t.Fatalf("Room = %q, want launch room", cfg.Room)
	}
}

func TestSaveWritesRoomField(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := Config{Name: "ada", Room: "launch room", Server: "http://127.0.0.1:8787", Addr: "127.0.0.1:8787"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "room = launch room") {
		t.Fatalf("saved config missing room field:\n%s", string(b))
	}
}

func TestWriteTokenRepairsDeletedPWD(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PWD", repo)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	if err := WriteToken("roomtok"); err != nil {
		t.Fatal(err)
	}
	if got := ReadToken(); got != "roomtok" {
		t.Fatalf("token = %q, want roomtok", got)
	}
	if _, err := os.Stat(filepath.Join(repo, TokenPath)); err != nil {
		t.Fatal(err)
	}
}
