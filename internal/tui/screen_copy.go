package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexisiba/asdfbkp/internal/backup"
)

// maxErrorsShown is how many failed files the result screen lists.
const maxErrorsShown = 5

type (
	copyProgressMsg backup.Progress
	copyDoneMsg     struct {
		res backup.Result
		err error
	}
)

// startCopy runs the backup in the background. Progress comes back as
// messages through a channel, so the interface never blocks.
func (m Model) startCopy() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan tea.Msg, 1)
	job := backup.Job{
		Source: m.src, Includes: m.includes, Dir: m.check.Dir, DeviceID: m.dev.ProfileKey(),
		DeviceName: m.dev.Name, Version: m.opts.Version,
	}
	go func() {
		res, err := backup.Run(ctx, job, func(p backup.Progress) {
			select {
			case ch <- copyProgressMsg(p):
			default: // the interface is busy; a newer snapshot will follow
			}
		})
		ch <- copyDoneMsg{res: res, err: err}
	}()
	m.copyCh, m.copyCancel = ch, cancel
	m.progress = backup.Progress{Scanning: true}
	m.confirmCancel, m.canceling = false, false
	m.screen = screenCopy
	return m, tea.Batch(m.spinner.Tick, waitCopy(ch))
}

func waitCopy(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m Model) updateCopy(key string) (tea.Model, tea.Cmd) {
	if m.canceling {
		return m, nil // waiting for the copy to stop
	}
	if m.confirmCancel {
		m.confirmCancel = false
		if key == "y" {
			m.canceling = true
			m.copyCancel()
		}
		return m, nil
	}
	switch key {
	case "esc", "q", "ctrl+c":
		m.confirmCancel = true
	}
	return m, nil
}

func (m Model) viewCopy() string {
	var b strings.Builder
	w := m.contentWidth()
	p := m.progress
	b.WriteString(wrap(selectedStyle.Render("Backing up "+m.dev.Name), w) + "\n")
	b.WriteString(row(w, col{w: 2, s: dim.Render("→")}, col{s: dim.Render(safe(tildify(m.check.Dir)) + "/")}) + "\n\n")

	if p.Scanning {
		b.WriteString(wrap(fmt.Sprintf("%s Looking for new and changed files… %d found", m.spinner.View(), p.Found), w) + "\n")
	} else {
		pct := 1.0
		if p.BytesTotal > 0 {
			pct = float64(p.BytesDone) / float64(p.BytesTotal)
		}
		m.bar.Width = max(w-8, 10)
		b.WriteString(m.bar.ViewAs(min(pct, 1)) + "\n\n")

		copied := humanSize(p.BytesDone) + " of " + humanSize(p.BytesTotal)
		if speed, left, ok := rate(p); ok {
			copied += fmt.Sprintf(" · %s/s · %s left", humanSize(int64(speed)), left)
		}
		field := func(label, value string) {
			b.WriteString(row(w, col{w: 12, s: dim.Render(label)}, col{s: value}) + "\n")
		}
		field("Copied", copied)
		field("Files", filesLine(p))
		if p.Current != "" {
			field("Current", safe(p.Current))
		}
	}

	switch {
	case m.canceling:
		b.WriteString("\n" + wrap(warn.Render("Canceling…"), w) + "\n")
	case m.confirmCancel:
		b.WriteString("\n" + wrap(warn.Render("Cancel the backup? Files already copied are kept. y/n"), w) + "\n")
	}
	b.WriteString("\n" + footer(w, "esc cancel"))
	return b.String()
}

func filesLine(p backup.Progress) string {
	s := fmt.Sprintf("%d of %d copied · %d up to date", p.Copied, p.ToCopy, p.UpToDate)
	if p.Failed > 0 {
		s += warn.Render(fmt.Sprintf(" · %d failed", p.Failed))
	}
	return s
}

// rate estimates the copy speed in bytes per second and the time left.
func rate(p backup.Progress) (speed float64, left string, ok bool) {
	elapsed := time.Since(p.Started)
	if p.Started.IsZero() || elapsed < time.Second || p.BytesDone == 0 {
		return 0, "", false
	}
	speed = float64(p.BytesDone) / elapsed.Seconds()
	remaining := time.Duration(float64(p.BytesTotal-p.BytesDone) / speed * float64(time.Second))
	return speed, remaining.Round(time.Second).String(), true
}

func (m Model) updateDone(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "enter":
		// Scan again: the devices may have changed.
		m.endSession()
		m.screen = screenScanning
		return m, tea.Batch(m.spinner.Tick, scan)
	}
	return m, nil
}

func (m Model) viewDone() string {
	var b strings.Builder
	w := m.contentWidth()
	res := m.copyResult

	switch {
	case m.copyErr != nil:
		b.WriteString(wrap(warn.Render("✗ Backup stopped"), w) + "\n")
		b.WriteString(wrap(safe(stoppedReason(m.copyErr)), w) + "\n")
	case res.Canceled:
		b.WriteString(wrap(warn.Render("✗ Backup canceled"), w) + "\n")
		b.WriteString(wrap(dim.Render("Files already copied are kept. The next backup continues where this one stopped."), w) + "\n")
	case res.Failed > 0:
		b.WriteString(wrap(warn.Render(fmt.Sprintf("⚠ Backup finished with %d problem%s", res.Failed, plural(res.Failed))), w) + "\n")
	default:
		b.WriteString(accent.Render("✓ ") + selectedStyle.Render("Backup complete") + "\n")
	}
	b.WriteString("\n")

	field := func(label, value string) {
		b.WriteString(row(w, col{w: 2}, col{w: 12, s: dim.Render(label)}, col{s: value}) + "\n")
	}
	field("Folder", safe(tildify(m.check.Dir))+"/")
	field("Copied", fmt.Sprintf("%d file%s · %s", res.Copied, plural(res.Copied), humanSize(res.BytesCopied)))
	field("Up to date", fmt.Sprintf("%d file%s", res.UpToDate, plural(res.UpToDate)))
	if res.Failed > 0 {
		field("Failed", warn.Render(fmt.Sprintf("%d file%s", res.Failed, plural(res.Failed))))
	}
	field("Time", res.Elapsed.Round(time.Second).String())

	if len(res.Errors) > 0 {
		b.WriteString("\n" + dim.Render("Problems:") + "\n")
		for _, e := range res.Errors[:min(len(res.Errors), maxErrorsShown)] {
			b.WriteString(row(w, col{w: 2, s: warn.Render("✗")}, col{s: safe(e.Rel) + dim.Render(": "+safe(e.Err.Error()))}) + "\n")
		}
		if extra := res.Failed - maxErrorsShown; extra > 0 {
			b.WriteString(row(w, col{w: 2}, col{s: dim.Render(fmt.Sprintf("and %d more", extra))}) + "\n")
		}
	}

	// Where to find the full report of the problems.
	switch {
	case res.LogErr != nil:
		b.WriteString("\n" + wrap(warn.Render("The log of the problems could not be saved: "+safe(res.LogErr.Error())), w) + "\n")
	case res.LogPath != "":
		b.WriteString("\n" + wrap("The full list of problems was saved in this folder:", w) + "\n")
		b.WriteString(row(w, col{w: 2}, col{s: profileNameStyle.Render(safe(tildify(filepath.Dir(res.LogPath))) + "/")}) + "\n")
		b.WriteString(row(w, col{w: 2}, col{s: dim.Render("file " + safe(filepath.Base(res.LogPath)))}) + "\n")
	}

	for _, n := range m.readyNotes {
		b.WriteString("\n" + wrap(n, w))
	}
	b.WriteString("\n\n" + footer(w, "enter back to start", "q quit"))
	return b.String()
}

func stoppedReason(err error) string {
	switch {
	case errors.Is(err, backup.ErrNoSpace), errors.Is(err, backup.ErrTooManyFail):
		return err.Error()
	}
	return "Could not complete the backup: " + err.Error()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
