package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	figure "github.com/common-nighthawk/go-figure"
)

const (
	appName    = "asdfbkp"
	appTagline = "general backup manager"
	bannerFont = "small"
)

var (
	logo = strings.TrimRight(figure.NewFigure(appName, bannerFont, true).String(), "\n ")

	bannerBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Padding(0, 3)
	bannerLogo = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
)

// banner draws the box with the logo, tagline and version. If the terminal is
// too narrow for the ASCII logo, it falls back to the plain name.
func banner(version string, width int) string {
	name := bannerLogo.Render(logo)
	if width > 0 && lipgloss.Width(name)+bannerBox.GetHorizontalFrameSize() > width {
		name = title.Render(appName)
	}
	body := lipgloss.JoinVertical(lipgloss.Center,
		name,
		"",
		appTagline,
		dim.Render("v"+version),
	)
	return bannerBox.Render(body)
}
