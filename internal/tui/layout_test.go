package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/profile"
)

func TestDeviceScreenFitsTerminalWidth(t *testing.T) {
	res := device.Result{
		Devices: []device.Device{
			{Kind: device.KindADB, Name: "Samsung Galaxy S24 Ultra", Path: "adb-R58M123ABC-xYz12._adb-tls-connect._tcp", State: "device", Wireless: true},
			{Kind: device.KindMTP, Name: "Xiaomi Redmi Note 13 Pro Plus 5G", Path: "/run/user/1000/gvfs/mtp:host=Xiaomi"},
			{Kind: device.KindUSB, Name: "Kingston DataTraveler Exodia Onyx", Path: "/media/user/Kingston DataTraveler Exodia Onyx", Size: 64 << 30},
		},
		Errors: []error{errors.New("lsblk: exit status 1: could not read the partition table of the device")},
	}
	for _, w := range []int{120, 80, 60, 45, 32} {
		var m tea.Model = New(Options{Version: "0.0.1"})
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
		m, _ = m.Update(scanDoneMsg(res))
		view := m.View()
		for i, line := range strings.Split(view, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width %d: line %d is %d columns: %q", w, i, lw, line)
			}
		}
		// Long text wraps inside its column; it is never cut.
		if strings.Contains(view, "…") {
			t.Errorf("width %d: text was truncated:\n%s", w, view)
		}
		for _, d := range res.Devices {
			if !strings.Contains(flat(view), d.Name) {
				t.Errorf("width %d: device name %q is incomplete", w, d.Name)
			}
		}
		// The layout is anchored to the left: the logo box starts at the same
		// column whatever the terminal width.
		for _, line := range strings.Split(view, "\n") {
			if i := strings.Index(line, "╭"); i >= 0 {
				if lipgloss.Width(line[:i]) > 2 {
					t.Errorf("width %d: header is not left-aligned:\n%s", w, view)
				}
				break
			}
		}
		if w == 60 || w == 32 {
			t.Logf("width %d:\n%s", w, view)
		}
	}
}

func TestTreeWrapsLongNamesAndKeepsCursorVisible(t *testing.T) {
	usb := t.TempDir()
	for i := 0; i < 30; i++ {
		write(t, filepath.Join(usb, fmt.Sprintf("Folder %02d with a really long name that will not fit in a narrow terminal", i), "a"), 1)
	}
	const w, h = 40, 24
	var m tea.Model = New(Options{Version: "0.0.1", Dest: t.TempDir()})
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.Update(scanDoneMsg(device.Result{Devices: []device.Device{{Kind: device.KindUSB, Name: "FOTOS", Path: usb}}}))
	m = press(t, m, "enter", "down", "enter")

	for i := 0; i < 29; i++ {
		view := m.View()
		lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
		if len(lines) > h {
			t.Fatalf("step %d: view is %d lines, terminal has %d:\n%s", i, len(lines), h, view)
		}
		if !strings.Contains(view, "›") {
			t.Fatalf("step %d: cursor scrolled out of view:\n%s", i, view)
		}
		if strings.Contains(view, "…") && !strings.Contains(view, "measuring…") {
			t.Fatalf("step %d: a name was truncated:\n%s", i, view)
		}
		m = press(t, m, "down")
	}
	if !strings.Contains(m.View(), "› [ ] ▸ Folder 29") {
		t.Errorf("the last folder should be visible at the end:\n%s", m.View())
	}
	t.Logf("\n%s", m.View())
}

func TestModeMenuWithLongProfileNamesFits(t *testing.T) {
	usb := t.TempDir()
	write(t, filepath.Join(usb, "a"), 1)
	store, err := profile.Load(filepath.Join(t.TempDir(), "profiles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Photos and videos from the trip", "Work documents and invoices 2026", "Everything except music folder"} {
		if err := store.Save("uuid:T", "FOTOS", profile.Profile{Name: n, Include: []string{"DCIM/Camera", "Documents/Work", "Pictures", "Download"}}, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []int{80, 32} {
		var m tea.Model = New(Options{Version: "0.0.1", Profiles: store})
		m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
		m, _ = m.Update(scanDoneMsg(device.Result{Devices: []device.Device{{Kind: device.KindUSB, ID: "uuid:T", Name: "FOTOS", Path: usb}}}))
		m = press(t, m, "enter")
		view := m.View()
		for i, line := range strings.Split(view, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width %d: line %d is %d columns: %q", w, i, lw, line)
			}
		}
		if strings.Contains(view, "…") {
			t.Errorf("width %d: text was truncated:\n%s", w, view)
		}
		for _, n := range []string{"Photos and videos", "Work documents", "Everything except"} {
			if !strings.Contains(view, n) {
				t.Errorf("width %d: profile %q missing:\n%s", w, n, view)
			}
		}
		if w == 32 {
			t.Logf("\n%s", view)
		}
	}
}
