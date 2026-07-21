package dash

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

func color(light, dark string) lipgloss.TerminalColor {
	if os.Getenv("NO_COLOR") != "" {
		return lipgloss.NoColor{}
	}
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	textColor    = color("#1F2937", "#E5E7EB")
	mutedColor   = color("#6B7280", "#9CA3AF")
	borderColor  = color("#D1D5DB", "#374151")
	accentBlue   = color("#2563EB", "#93C5FD")
	accentGreen  = color("#15803D", "#86EFAC")
	accentCoral  = color("#DC2626", "#FCA5A5")
	accentAmber  = color("#B45309", "#FCD34D")
	accentCyan   = color("#0F766E", "#5EEAD4")
	offlineColor = color("#9CA3AF", "#4B5563")

	shell = lipgloss.NewStyle().
		Foreground(textColor).
		Padding(1, 2, 0, 2)

	headerBar = lipgloss.NewStyle().
			Foreground(textColor).
			Bold(true).
			Padding(0, 1)

	flashBar = lipgloss.NewStyle().
			Foreground(accentCoral).
			Bold(true).
			Padding(0, 1)

	panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	sectionTitle = lipgloss.NewStyle().
			Foreground(accentGreen).
			Bold(true)

	tabStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(borderColor).
			Padding(0, 2)

	activeTabStyle = tabStyle.
			Foreground(textColor).
			Bold(true).
			BorderForeground(accentCyan)

	filterIdle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	filterActive = lipgloss.NewStyle().
			Foreground(textColor).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(accentCyan).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			PaddingTop(1)

	accent      = lipgloss.NewStyle().Foreground(accentCyan)
	accentStyle = lipgloss.NewStyle().Foreground(accentCyan).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(mutedColor)
	textStyle   = lipgloss.NewStyle().Foreground(textColor)
	nameStyle   = lipgloss.NewStyle().Foreground(textColor).Bold(true)
	lockStyle   = lipgloss.NewStyle().Foreground(accentAmber)
	metricStyle = lipgloss.NewStyle().Foreground(accentGreen).Bold(true)
	badgeStyle  = lipgloss.NewStyle().Foreground(accentCyan).Bold(true)

	onlineDot  = lipgloss.NewStyle().Foreground(accentGreen)
	offlineDot = lipgloss.NewStyle().Foreground(offlineColor)

	footStyle = lipgloss.NewStyle().Foreground(accentCyan)
	errStyle  = lipgloss.NewStyle().Foreground(accentCoral).Bold(true)
)
