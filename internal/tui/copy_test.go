package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alexisiba/asdfbkp/internal/backup"
	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/source"
)

// copying returns a model on the copy screen with a fake running copy.
func copying(t *testing.T, w int) (Model, *bool) {
	t.Helper()
	canceled := false
	m := New(Options{Version: "0.0.1"})
	m.width, m.height = w, 40
	m.dev = device.Device{Kind: device.KindUSB, Name: "Kingston DataTraveler Exodia Onyx", Path: "/media/u/K"}
	m.src = source.For(m.dev)
	m.check.Dir = "/home/u/Backups/a/very/long/destination/path/Kingston-DataTraveler-Exodia-Onyx"
	m.screen = screenCopy
	m.copyCancel = func() { canceled = true }
	m.progress = backup.Progress{
		ToCopy: 540, Copied: 312, UpToDate: 1204, Failed: 2,
		BytesTotal: 2 << 30, BytesDone: 1 << 30, Started: time.Now().Add(-30 * time.Second),
		Current: "DCIM/Camera/2026/October/Trip to the mountains/IMG_20261014_183512_HDR.jpg",
	}
	return m, &canceled
}

func key(m tea.Model, k string) (tea.Model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	switch k {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return m.Update(msg)
}

func TestCancelAsksFirst(t *testing.T) {
	m, canceled := copying(t, 80)

	next, cmd := key(m, "ctrl+c")
	if cmd != nil {
		t.Fatal("ctrl+c during a copy must not quit right away")
	}
	if !strings.Contains(next.View(), "Cancel the backup? Files already copied are kept. y/n") {
		t.Fatalf("ctrl+c should ask for confirmation:\n%s", next.View())
	}
	next, _ = key(next, "n")
	if *canceled || next.(Model).confirmCancel {
		t.Fatal("n must keep the copy running")
	}
	next, _ = key(next, "esc")
	next, _ = key(next, "y")
	if !*canceled || !strings.Contains(next.View(), "Canceling…") {
		t.Fatalf("y must cancel the copy:\n%s", next.View())
	}
}

func TestCopyScreensFitNarrowTerminals(t *testing.T) {
	for _, w := range []int{80, 32} {
		m, _ := copying(t, w)
		views := map[string]string{"progress": m.View()}

		m.screen = screenDone
		m.copyResult = backup.Result{Progress: m.progress, Elapsed: 83 * time.Second,
			LogPath: "/home/u/Backups/Kingston/serial-ABC-uuid-1234-logs/2026-10-14_15-30-05.log", Errors: []backup.FileError{
				{Rel: "DCIM/Camera/very long file name that keeps going.jpg", Err: errors.New("the file is too large for the destination's filesystem (FAT32 allows up to 4 GiB)")},
				{Rel: "Documents/b.pdf", Err: errors.New("permission denied")},
			}}
		views["result"] = m.View()

		for name, view := range views {
			for i, line := range strings.Split(view, "\n") {
				if lw := lipgloss.Width(line); lw > w {
					t.Errorf("%s at width %d: line %d is %d columns: %q", name, w, i, lw, line)
				}
			}
			if strings.Contains(flat(view), "IMG_20261014") && name == "progress" && !strings.Contains(flat(view), "HDR.jpg") {
				t.Errorf("%s at width %d: current file was cut", name, w)
			}
			if w == 80 {
				t.Logf("%s:\n%s", name, view)
			}
		}
		if v := flat(views["result"]); !strings.Contains(v, "⚠ Backup finished with 2 problems") || !strings.Contains(v, "permission denied") ||
			!strings.Contains(v, "saved in this folder:") || !strings.Contains(v, "2026-10-14_15-30-05.log") {
			t.Errorf("result should list the problems:\n%s", views["result"])
		}
	}
}
