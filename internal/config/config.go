// Package config is the whole clubhouse setup in one human-editable text file:
// .clubhouse/config.txt (key = value, # for comments).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clubhouse/internal/names"
	"clubhouse/internal/room"
)

const (
	Dir         = ".clubhouse"
	ConfigPath  = Dir + "/config.txt"
	TokenPath   = Dir + "/token"
	SnapPath    = Dir + "/room.json"
	SessionPath = Dir + "/session" // this member's id, shared by hook + mcp
)

func ReadSession() string {
	_ = ensureWorkingDir()
	b, _ := os.ReadFile(SessionPath)
	return strings.TrimSpace(string(b))
}

func WriteSession(id string) error {
	if err := ensureWorkingDir(); err != nil {
		return err
	}
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(SessionPath, []byte(id), 0o600)
}

type Config struct {
	Name   string // your member/display name (blank -> a random one)
	Room   string // host role only: clubhouse/room name shown in the lounge
	Server string // clubhouse URL a friend sent you
	Addr   string // host role only: what to listen on
}

func Defaults() Config {
	return Config{Addr: "127.0.0.1:8787"}
}

// template is what onboarding writes. It IS the documentation — a labeled form
// a non-coder can fill in and save.
const template = `# clubhouse config — edit a value, save the file, done.

# your name in the clubhouse (blank = a random one is picked for you)
name = %s

# host only — the clubhouse room name shown in the lounge
room = %s

# clubhouse link a friend sent you
server = %s

# host only — the address to listen on
addr = %s
`

// Parse reads key=value lines. Unknown keys are ignored so the format can grow.
func Parse(text string) Config {
	c := Defaults()
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key, val = strings.TrimSpace(strings.ToLower(key)), strings.TrimSpace(val); key {
		case "name":
			c.Name = val
		case "room", "clubhouse", "clubhouse_name":
			c.Room = val
		case "server":
			c.Server = val
		case "addr":
			c.Addr = val
		}
	}
	return c
}

func Load() (Config, error) {
	if err := ensureWorkingDir(); err != nil {
		return Defaults(), err
	}
	b, err := os.ReadFile(ConfigPath)
	if err != nil {
		return Defaults(), err
	}
	return Parse(string(b)), nil
}

func LoadOrDefault() Config {
	if c, err := Load(); err == nil {
		return c
	}
	return Defaults()
}

func (c Config) Save() error {
	if err := ensureWorkingDir(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(template, c.Name, c.Room, c.Server, c.Addr)
	return os.WriteFile(ConfigPath, []byte(body), 0o644)
}

// EnsureName gives a blank config a random handle and persists it, so a member
// keeps the same name across restarts.
func (c *Config) EnsureName() {
	if strings.TrimSpace(c.Name) == "" {
		c.Name = names.Random()
		c.Save()
	}
}

// EnsureRoom gives a host a stable room name that is separate from their member
// name. The default follows the repo directory so the lounge header is useful.
func (c *Config) EnsureRoom() {
	if strings.TrimSpace(c.Room) == "" {
		c.Room = DefaultRoomName()
		c.Save()
	}
}

func DefaultRoomName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "the-clubhouse"
	}
	base := filepath.Base(wd)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "the-clubhouse"
	}
	return base + " clubhouse"
}

func LoadOrCreateToken() string {
	_ = ensureWorkingDir()
	if b, err := os.ReadFile(TokenPath); err == nil && len(b) > 0 {
		return strings.TrimSpace(string(b))
	}
	tok := room.NewID()
	os.MkdirAll(Dir, 0o755)
	os.WriteFile(TokenPath, []byte(tok), 0o600)
	return tok
}

func ReadToken() string {
	_ = ensureWorkingDir()
	b, _ := os.ReadFile(TokenPath)
	return strings.TrimSpace(string(b))
}

func WriteToken(tok string) error {
	if err := ensureWorkingDir(); err != nil {
		return err
	}
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(TokenPath, []byte(tok), 0o600)
}

func ensureWorkingDir() error {
	if wd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(wd); err == nil {
			return nil
		}
	}
	pwd := os.Getenv("PWD")
	if pwd == "" || !filepath.IsAbs(pwd) {
		return fmt.Errorf("current directory no longer exists; cd into the repo and retry")
	}
	pwd = filepath.Clean(pwd)
	if err := os.MkdirAll(pwd, 0o755); err != nil {
		return fmt.Errorf("current directory no longer exists; cd into the repo and retry: %w", err)
	}
	if err := os.Chdir(pwd); err != nil {
		return fmt.Errorf("current directory no longer exists; cd into the repo and retry: %w", err)
	}
	return nil
}
