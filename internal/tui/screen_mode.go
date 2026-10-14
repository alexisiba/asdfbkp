package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/profile"
)

type modeKind int

const (
	modeProfile modeKind = iota
	modeFull
	modeCustom
	modeSeparator // a divider line, never selectable
)

type modeOption struct {
	kind    modeKind
	label   string
	detail  string
	profile profile.Profile // modeProfile only
}

func (m Model) modeOptions() []modeOption {
	var opts []modeOption
	ps := m.profiles()
	for _, p := range ps {
		opts = append(opts, modeOption{kind: modeProfile, label: safe(p.Name), profile: p,
			detail: summarizeIncludes(p.Include) + " · " + p.Updated.Format("Jan 2, 2006")})
	}
	if len(ps) > 0 {
		opts = append(opts, modeOption{kind: modeSeparator})
	}
	full := "the whole drive"
	if m.dev.Kind != device.KindUSB {
		full = "all storage (" + safe(m.src.Root()) + ")"
	}
	opts = append(opts,
		modeOption{kind: modeFull, label: "Full backup", detail: full},
		modeOption{kind: modeCustom, label: "Custom backup", detail: "choose folders"},
	)
	return opts
}

func summarizeIncludes(inc []string) string {
	const maxShown = 3
	if len(inc) == 1 && inc[0] == "." {
		return "everything"
	}
	shown := inc
	if len(shown) > maxShown {
		shown = shown[:maxShown]
	}
	s := safe(strings.Join(shown, ", "))
	if extra := len(inc) - len(shown); extra > 0 {
		s += fmt.Sprintf(" and %d more", extra)
	}
	return s
}

// moveModeCursor moves the cursor by delta, skipping the separator.
func (m *Model) moveModeCursor(delta int) {
	opts := m.modeOptions()
	c := max(0, min(m.modeCursor+delta, len(opts)-1))
	if opts[c].kind == modeSeparator {
		if c+delta >= 0 && c+delta < len(opts) {
			c += delta
		} else {
			c = m.modeCursor
		}
	}
	m.modeCursor = c
}

func (m Model) currentModeOption() modeOption {
	opts := m.modeOptions()
	return opts[min(m.modeCursor, len(opts)-1)]
}

func (m Model) updateMode(key string) (tea.Model, tea.Cmd) {
	if m.confirmDelete != "" {
		return m.updateConfirmDelete(key)
	}
	opt := m.currentModeOption()
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		m.endSession()
		m.screen = screenDevices
	case "?":
		m.helpFrom = screenMode
		m.screen = screenHelp
	case "up", "k":
		m.moveModeCursor(-1)
	case "down", "j":
		m.moveModeCursor(1)
	case "e":
		if opt.kind == modeProfile {
			return m.openTree(opt.profile.Name, opt.profile.Include)
		}
	case "x":
		if opt.kind == modeProfile {
			m.confirmDelete = opt.profile.Name
		}
	case "enter", " ":
		switch opt.kind {
		case modeProfile:
			m.mode, m.usingProfile, m.editingProfile = modeProfile, opt.profile.Name, ""
			return m.enterSummary(opt.profile.Include)
		case modeFull:
			m.mode, m.usingProfile, m.editingProfile = modeFull, "", ""
			return m.enterSummary([]string{"."})
		case modeCustom:
			return m.openTree("", nil)
		}
	}
	return m, nil
}

// openTree opens the folder picker for a new custom selection (editing "")
// or to edit a profile. Going back and forth keeps the same tree.
func (m Model) openTree(editing string, initial []string) (tea.Model, tea.Cmd) {
	m.screen = screenTree
	m.usingProfile = ""
	if m.tree != nil && m.mode == modeCustom && m.editingProfile == editing {
		return m, nil
	}
	m.mode, m.editingProfile = modeCustom, editing
	m.saveAsNew, m.nameEdited = false, false
	m.tree = newTree(initial)
	return m, listCmd(m.ctx, m.src, ".")
}

func (m Model) updateConfirmDelete(key string) (tea.Model, tea.Cmd) {
	name := m.confirmDelete
	m.confirmDelete = ""
	if key != "y" {
		return m, nil
	}
	if err := m.opts.Profiles.Delete(m.dev.ProfileKey(), name); err != nil {
		m.notice = "Could not delete the profile: " + err.Error()
		return m, nil
	}
	if m.editingProfile == name {
		m.tree, m.editingProfile = nil, ""
	}
	m.notice = fmt.Sprintf("Profile %q deleted.", name)
	m.modeCursor = 0
	m.moveModeCursor(0)
	return m, nil
}

func (m Model) viewMode() string {
	var b strings.Builder
	w := m.contentWidth()
	badge := kindBadgePlain(m.dev.Kind)
	b.WriteString(row(w,
		col{w: lipgloss.Width(badge) + 1, s: badge},
		col{s: selectedStyle.Render(m.dev.Name) + deviceDetail(m.dev)},
	) + "\n\n")

	for i, o := range m.modeOptions() {
		if o.kind == modeSeparator {
			b.WriteString(row(w, col{w: 2}, col{s: dim.Render("─────")}) + "\n")
			continue
		}
		cur, label := "  ", o.label
		if i == m.modeCursor {
			cur, label = accent.Render("› "), selectedStyle.Render(label)
		}
		if w < narrowWidth {
			// Too narrow for two text columns: the detail goes under its label.
			b.WriteString(row(w, col{w: 2, s: cur}, col{s: label}) + "\n")
			b.WriteString(row(w, col{w: 4}, col{s: dim.Render(o.detail)}) + "\n")
			continue
		}
		b.WriteString(row(w, col{w: 2, s: cur}, col{w: 25, s: label}, col{s: dim.Render(o.detail)}) + "\n")
	}

	if m.confirmDelete != "" {
		b.WriteString("\n" + wrap(warn.Render(fmt.Sprintf("Delete profile %q? y/n", safe(m.confirmDelete))), w) + "\n")
	} else if m.notice != "" {
		b.WriteString("\n" + wrap(warn.Render(safe(m.notice)), w) + "\n")
	}
	if m.dev.Kind == device.KindMTP {
		b.WriteString("\n" + wrap(mtpWarning, w) + "\n")
	}

	keys := []string{"↑/↓ move", "enter select"}
	if m.currentModeOption().kind == modeProfile {
		keys = append(keys, "e edit", "x delete")
	}
	if m.dev.Kind == device.KindMTP {
		keys = append(keys, "? Android help")
	}
	keys = append(keys, "esc back", "q quit")
	b.WriteString("\n" + footer(w, keys...))
	return b.String()
}
