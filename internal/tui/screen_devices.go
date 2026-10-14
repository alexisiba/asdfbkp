package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/human"
)

func (m Model) updateDevices(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.devCursor = max(m.devCursor-1, 0)
	case "down", "j":
		m.devCursor = min(m.devCursor+1, max(len(m.result.Devices)-1, 0))
	case "r":
		m.screen = screenScanning
		return m, tea.Batch(m.spinner.Tick, scan)
	case "?":
		m.helpFrom = screenDevices
		m.screen = screenHelp
	case "enter", " ":
		if len(m.result.Devices) == 0 {
			return m, nil
		}
		d := m.result.Devices[m.devCursor]
		if !d.Ready() {
			m.notice = adbStateHint(d.State)
			return m, nil
		}
		m.startSession(d)
		m.screen = screenMode
	}
	return m, nil
}

func (m Model) updateHelp(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "?", "enter", "backspace":
		m.screen = m.helpFrom
	}
	return m, nil
}

func (m Model) viewDevices() string {
	var b strings.Builder
	devs := m.result.Devices
	w := m.contentWidth()

	if len(devs) == 0 {
		b.WriteString("No devices found.\n")
		b.WriteString(wrap(dim.Render("Plug in a USB drive or a phone and press r to scan again."), w) + "\n")
	} else {
		b.WriteString("Choose what to back up:\n\n")
	}

	hasMTP := false
	for i, d := range devs {
		if d.Kind == device.KindMTP {
			hasMTP = true
		}
		cur := "  "
		name := d.Name
		if i == m.devCursor {
			cur = accent.Render("› ")
			name = selectedStyle.Render(name)
		}
		badge := kindBadge(d.Kind, w)
		b.WriteString(row(w,
			col{w: 2, s: cur},
			col{w: lipgloss.Width(badge) + 1, s: badge},
			col{s: name + deviceDetail(d)},
		) + "\n")
	}

	para := func(s string) { b.WriteString("\n" + wrap(s, w) + "\n") }
	if m.notice != "" {
		para(warn.Render(safe(m.notice)))
	}
	if hasMTP {
		para(mtpWarning)
	} else if !m.result.ADBAvailable {
		para(dim.Render("adb is not installed: phones over USB/wireless debugging were not scanned."))
	}
	if m.opts.ProfileWarning != "" {
		para(warn.Render(safe(m.opts.ProfileWarning)))
	}
	for _, err := range m.result.Errors {
		para(warn.Render("Error: " + safe(err.Error())))
	}

	b.WriteString("\n" + footer(w, "↑/↓ move", "enter select", "r rescan", "? Android help", "q quit"))
	return b.String()
}

func deviceDetail(d device.Device) string {
	switch d.Kind {
	case device.KindUSB:
		return dim.Render(fmt.Sprintf("  %s · %s", safe(d.Path), humanSize(int64(d.Size))))
	case device.KindADB:
		if d.State != "device" {
			return "  " + warn.Render(d.State)
		}
		if d.Wireless {
			return dim.Render("  wireless · " + safe(d.Path))
		}
		return dim.Render("  " + safe(d.Path))
	case device.KindMTP:
		return "  " + warn.Render("slow")
	}
	return ""
}

func adbStateHint(state string) string {
	switch state {
	case "unauthorized":
		return "The phone hasn't authorized this computer: accept the debugging prompt on its screen and press r."
	case "offline":
		return "The phone shows as offline: reconnect it or restart debugging, then press r."
	}
	return fmt.Sprintf("The phone is not ready (%s). Press r to scan again.", state)
}

var humanSize = human.Size
