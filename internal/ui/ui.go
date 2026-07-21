// Package ui holds the little bits of terminal flair shared across commands.
package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	ink    = terminalColor("#1F2937", "#E5E7EB")
	dim    = terminalColor("#6B7280", "#9CA3AF")
	accent = terminalColor("#0F766E", "#5EEAD4")
	good   = terminalColor("#15803D", "#86EFAC")

	box   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 3)
	title = lipgloss.NewStyle().Foreground(ink).Bold(true)
	tag   = lipgloss.NewStyle().Foreground(dim).Italic(true)
	ok    = lipgloss.NewStyle().Foreground(good).Bold(true)
	link  = lipgloss.NewStyle().Foreground(accent)
	hint  = lipgloss.NewStyle().Foreground(dim)
)

func terminalColor(light, dark string) lipgloss.TerminalColor {
	if os.Getenv("NO_COLOR") != "" {
		return lipgloss.NoColor{}
	}
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// Banner is the little splash shown on serve/setup/first run.
func Banner() string {
	return box.Render(
		title.Render("clubhouse") + "\n" +
			tag.Render("your team's Codex agents, in one room"))
}

func OK(s string) string   { return ok.Render("ok " + s) }
func Link(s string) string { return link.Render(s) }
func Hint(s string) string { return hint.Render(s) }
