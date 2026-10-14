package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/alexisiba/asdfbkp/internal/device"
)

var (
	accentColor = lipgloss.AdaptiveColor{Light: "#5A3FD9", Dark: "#9D8CFF"}
	warnColor   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	dimColor    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#8B8FA3"}

	title         = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	accent        = lipgloss.NewStyle().Foreground(accentColor)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	dim           = lipgloss.NewStyle().Foreground(dimColor)
	warn          = lipgloss.NewStyle().Foreground(warnColor)
	// profileNameStyle highlights a profile name inside a sentence.
	profileNameStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	box              = lipgloss.NewStyle().Padding(1, 2)
	boxNarrow        = lipgloss.NewStyle().Padding(0, 1)
	badge            = lipgloss.NewStyle().Width(15)
	badgeShort       = lipgloss.NewStyle().Width(6)
)

// narrowWidth is the content width below which the UI compacts itself:
// short badges and less padding.
const narrowWidth = 56

// kindBadge is the aligned badge of the device list. In narrow windows it uses
// the short form: [ADB] instead of [Android (ADB)].
func kindBadge(k device.Kind, w int) string {
	s, label := badge, k.String()
	if w < narrowWidth {
		s, label = badgeShort, shortKind(k)
	}
	s = s.Foreground(accentColor)
	if k == device.KindMTP {
		s = s.Foreground(warnColor)
	}
	return s.Render("[" + label + "]")
}

func shortKind(k device.Kind) string {
	switch k {
	case device.KindADB:
		return "ADB"
	case device.KindMTP:
		return "MTP"
	}
	return "USB"
}

// safe prepares text that comes from a device for the terminal.
var safe = device.Printable

// kindBadgePlain is the badge without the fixed width that aligns the list.
func kindBadgePlain(k device.Kind) string {
	s := accent
	if k == device.KindMTP {
		s = warn
	}
	return s.Render("[" + k.String() + "]")
}

var mtpWarning = warn.Render("⚠ Android over MTP is slow and may drop in the middle of a backup.") +
	" For a fast and reliable backup, enable " + selectedStyle.Render("USB debugging") +
	" or " + selectedStyle.Render("wireless debugging") + ". Press ? to see how."

const helpText = `How to set up your Android phone (recommended method)

1. Enable developer options
   Settings → About phone → tap "Build number" 7 times.

2a. USB debugging
   Settings → Developer options → turn on "USB debugging".
   Plug in the cable and accept the "Allow USB debugging?" prompt.

2b. Wireless debugging (Android 11 or later)
   Settings → Developer options → "Wireless debugging".
   Tap "Pair device with pairing code" and run on the computer:
     adb pair IP:PORT       (the port and code shown by the phone)
     adb connect IP:PORT    (the port on the main screen)

You need adb installed on the computer (the android-tools-adb package
or Android SDK Platform-Tools). Then press r in asdfbkp.
`
