// Package clog centralizes clubhouse terminal logging.
package clog

import (
	"io"
	"os"
	"strings"
	"time"

	charmlog "github.com/charmbracelet/log"
)

// New returns a Charm logger configured from CLUBHOUSE_LOG and
// CLUBHOUSE_LOG_FORMAT.
func New(w io.Writer) *charmlog.Logger {
	l := charmlog.NewWithOptions(w, charmlog.Options{
		ReportTimestamp: true,
		TimeFormat:      time.Kitchen,
		Prefix:          "clubhouse",
		Formatter:       formatter(),
	})
	l.SetLevel(level())
	return l
}

func Stderr() *charmlog.Logger {
	return New(os.Stderr)
}

func formatter() charmlog.Formatter {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLUBHOUSE_LOG_FORMAT"))) {
	case "json":
		return charmlog.JSONFormatter
	case "logfmt":
		return charmlog.LogfmtFormatter
	default:
		return charmlog.TextFormatter
	}
}

func level() charmlog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLUBHOUSE_LOG"))) {
	case "debug":
		return charmlog.DebugLevel
	case "warn", "warning":
		return charmlog.WarnLevel
	case "error":
		return charmlog.ErrorLevel
	default:
		return charmlog.InfoLevel
	}
}
