package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateGoMenu(t *testing.T) {
	root := t.TempDir()
	src := `package demo

import "fmt"

// Greeter says hello.
type Greeter struct {
	Name string
}

// NewGreeter builds a greeter.
func NewGreeter(name string) *Greeter {
	return &Greeter{Name: name}
}

// Say prints the greeting.
func (g *Greeter) Say() {
	helper()
	fmt.Println(g.Name)
}

func helper() {}
`
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Generate(Options{Root: root, Out: "menu", Clean: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Packages != 1 {
		t.Fatalf("packages = %d, want 1", res.Packages)
	}
	if res.Symbols != 4 {
		t.Fatalf("symbols = %d, want 4", res.Symbols)
	}

	index := read(t, filepath.Join(root, "menu", "index.md"))
	for _, want := range []string{"# Code Menu", "demo", "Packages"} {
		if !strings.Contains(index, want) {
			t.Fatalf("index missing %q:\n%s", want, index)
		}
	}

	say := read(t, filepath.Join(root, "menu", "symbols", "method-greeter-say.md"))
	for _, want := range []string{"# Greeter.Say", "helper", "fmt.Println"} {
		if !strings.Contains(say, want) {
			t.Fatalf("method page missing %q:\n%s", want, say)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
