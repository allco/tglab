package main

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// splashMin is how long the splash stays up at least, so it doesn't just flash when the first
// load is quick. It stays longer while that load is still running.
const splashMin = 1200 * time.Millisecond

var splashLogo = []string{
	"████████╗ ██████╗ ██╗      █████╗ ██████╗ ",
	"╚══██╔══╝██╔════╝ ██║     ██╔══██╗██╔══██╗",
	"   ██║   ██║  ███╗██║     ███████║██████╔╝",
	"   ██║   ██║   ██║██║     ██╔══██║██╔══██╗",
	"   ██║   ╚██████╔╝███████╗██║  ██║██████╔╝",
	"   ╚═╝    ╚═════╝ ╚══════╝╚═╝  ╚═╝╚═════╝ ",
}

// cornerLogo is the small logo the main view keeps in its bottom-right corner when the rows leave
// room for it.
var cornerLogo = []string{
	"╺┳╸ ┏━╸ ╻   ┏━┓ ┏┓ ",
	" ┃  ┃╺┓ ┃   ┣━┫ ┣┻┓",
	" ╹  ┗━┛ ┗━╸ ╹ ╹ ┗━┛",
}

// splashColors shades the logo top to bottom, from pink into orange.
var splashColors = []lipgloss.Color{"213", "212", "211", "210", "209", "208"}

type splashDoneMsg struct{}

func splashCmd() tea.Cmd {
	return tea.Tick(splashMin, func(time.Time) tea.Msg { return splashDoneMsg{} })
}

// splashView centres the logo, the version and the loading state in the window.
func (m model) splashView() string {
	var logo string
	if lipgloss.Width(splashLogo[0]) <= m.width {
		logo = strings.Join(logoLines(splashLogo), "\n")
	} else {
		logo = titleStyle.Render("tglab")
	}

	status := dimStyle.Render("GitLab merge requests in your terminal")
	if m.loading {
		status = m.spinner.View() + dimStyle.Render("loading merge requests…")
	}
	content := lipgloss.JoinVertical(lipgloss.Center,
		logo,
		"",
		dimStyle.Render(version+" · "+m.cfg.Host),
		"",
		status,
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// logoLines renders logo shaded like the splash.
func logoLines(logo []string) []string {
	lines := make([]string, len(logo))
	for i, l := range logo {
		c := splashColors[i*(len(splashColors)-1)/max(len(logo)-1, 1)]
		lines[i] = lipgloss.NewStyle().Bold(true).Foreground(c).Render(l)
	}
	return lines
}

// withCornerLogo puts the small logo at the right end of lines, one logo row per line, after
// whatever text the lines already have. It returns false, and lines unchanged, when any line is
// too long to leave room for it.
func withCornerLogo(lines []string, width int) ([]string, bool) {
	w := lipgloss.Width(cornerLogo[0])
	out := make([]string, len(lines))
	for i, l := range logoLines(cornerLogo) {
		left := lines[i]
		gap := width - 1 - w - lipgloss.Width(left)
		if gap < 2 {
			return lines, false
		}
		out[i] = left + strings.Repeat(" ", gap) + l
	}
	return out, true
}
