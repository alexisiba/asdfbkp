package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexisiba/asdfbkp/internal/dest"
	"github.com/alexisiba/asdfbkp/internal/profile"
)

func (m Model) inspectDest() dest.Check {
	dir := dest.DeviceDir(m.opts.Dest, m.dev.FolderName(), m.dev.ProfileKey())
	local, _ := m.src.LocalPath()
	return dest.Inspect(dir, local)
}

// blocker explains why the backup can't start yet, or returns "" if it can.
func (m Model) blocker() string {
	if len(m.check.Problems) > 0 {
		return m.check.Problems[0]
	}
	// Free space is checked by the copy engine against what actually needs
	// copying: an incremental backup may fit even when the total doesn't.
	return ""
}

func (m Model) updateSummary(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		if m.mode == modeCustom {
			m.screen = screenTree
		} else {
			m.screen = screenMode
		}
	case "d":
		m.editField = fieldDest
		m.destInput.SetValue(tildify(m.opts.Dest))
		m.destInput.CursorEnd()
		return m, m.destInput.Focus()
	case " ":
		if m.canSaveProfile() {
			m.saveProf = !m.saveProf
		}
	case "n":
		if m.canSaveProfile() && m.saveProf {
			m.editField = fieldName
			m.nameInput.SetValue(m.profileName)
			m.nameInput.CursorEnd()
			return m, m.nameInput.Focus()
		}
	case "s":
		if m.mode != modeCustom || m.editingProfile == "" {
			return m, nil
		}
		if !m.saveAsNew && m.atProfileLimit() {
			m.notice = profileLimitNotice
			return m, nil
		}
		m.saveAsNew = !m.saveAsNew
		m.nameEdited = false
		m.prepareSave()
	case "enter":
		if b := m.blocker(); b != "" {
			m.notice = b
			return m, nil
		}
		m.readyNotes = nil
		if m.saveProf && m.canSaveProfile() {
			replace := ""
			if !m.savingNew() {
				replace = m.editingProfile
			}
			p := profile.Profile{Name: m.profileName, Include: m.includes}
			if err := m.opts.Profiles.Save(m.dev.ProfileKey(), m.dev.Name, p, replace); err != nil {
				m.notice = saveErrorNotice(err, m.profileName)
				return m, nil
			}
			m.readyNotes = append(m.readyNotes, fmt.Sprintf("Profile %q saved.", safe(profile.CleanName(m.profileName))))
		}
		return m.startCopy()
	}
	return m, nil
}

const profileLimitNotice = "Profile limit reached (3): delete one from the menu with x."

func saveErrorNotice(err error, name string) string {
	switch {
	case errors.Is(err, profile.ErrLimit):
		return profileLimitNotice
	case errors.Is(err, profile.ErrNameTaken):
		return fmt.Sprintf("A profile named %q already exists. Press n to choose another name.", safe(name))
	}
	return "Could not save the profile: " + err.Error()
}

// updateField handles keys while a text field of the summary is being edited.
func (m Model) updateField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	input := &m.destInput
	if m.editField == fieldName {
		input = &m.nameInput
	}
	switch msg.String() {
	case "esc":
		m.editField = fieldNone
		input.Blur()
		return m, nil
	case "enter":
		if m.editField == fieldName {
			name := profile.CleanName(input.Value())
			switch {
			case name == "":
				m.notice = "The profile name can't be empty."
				return m, nil
			case m.nameTaken(name):
				m.notice = fmt.Sprintf("A profile named %q already exists.", safe(name))
				return m, nil
			}
			m.profileName, m.nameEdited = name, true
		} else {
			p, err := filepath.Abs(expandHome(strings.TrimSpace(input.Value())))
			if err != nil || strings.TrimSpace(input.Value()) == "" {
				m.notice = "Invalid path."
				return m, nil
			}
			m.opts.Dest = p
			m.check = m.inspectDest()
		}
		m.editField = fieldNone
		input.Blur()
		return m, nil
	case "tab":
		if m.editField == fieldDest {
			input.SetValue(completeDir(input.Value()))
			input.CursorEnd()
		}
		return m, nil
	}
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)
	return m, cmd
}

func (m Model) viewSummary() string {
	w := m.contentWidth()
	var b strings.Builder
	b.WriteString(selectedStyle.Render("Backup summary") + "\n\n")

	field := func(label, value string) {
		b.WriteString(row(w, col{w: 2}, col{w: 12, s: dim.Render(label)}, col{s: value}) + "\n")
	}
	para := func(s string) { b.WriteString("\n" + wrap(s, w)) }

	field("Source", kindBadgePlain(m.dev.Kind)+" "+m.dev.Name+dim.Render("  "+safe(m.src.Root())))

	if m.usingProfile != "" {
		field("Profile", safe(m.usingProfile))
	}

	if m.editField == fieldDest {
		m.destInput.Width = max(w-17, minFlexWidth)
		field("Destination", m.destInput.View())
		field("", dim.Render("tab complete · enter accept · esc cancel"))
		field("", dim.Render("will be saved to <destination>/"+m.dev.FolderName()))
	} else {
		d := safe(tildify(m.check.Dir)) + "/"
		if !m.check.Exists && len(m.check.Problems) == 0 {
			d += dim.Render("  (will be created)")
		}
		field("Destination", d)
		if base := filepath.Base(m.check.Dir); base != m.dev.FolderName() {
			field("", dim.Render("A folder named "+m.dev.FolderName()+" already exists and is not from this device: using "+base))
		}
	}

	field("Contents", describeIncludes(m.includes))

	total, ok := m.totalSize(m.includes)
	size := humanSize(total)
	if !ok {
		size = m.spinner.View() + " measuring…"
	}
	field("Size", size)

	if m.check.FreeKnown {
		free := humanSize(int64(m.check.Free)) + " free"
		switch {
		case !ok:
			field("Space", free)
		case uint64(total) > m.check.Free:
			field("Space", warn.Render(free+" · the selection is larger, but only new or changed files are copied"))
		default:
			field("Space", free+accent.Render("  ✓"))
		}
	}

	for _, rel := range m.includes {
		if s := m.sizes[rel]; s.done && s.err != nil {
			para(warn.Render(safe(rel) + " was not found on the device."))
		}
	}

	b.WriteString(m.viewSaveProfile(w))

	for _, p := range m.check.Problems {
		para(warn.Render("✗ " + safe(p)))
	}
	if m.notice != "" {
		para(warn.Render(safe(m.notice)))
	}

	keys := []string{"enter start", "d change destination"}
	if m.canSaveProfile() {
		keys = append(keys, "space select/unselect")
		if m.saveProf {
			keys = append(keys, "n rename profile")
		}
	}
	if m.mode == modeCustom && m.editingProfile != "" {
		if m.saveAsNew {
			keys = append(keys, "s update instead")
		} else {
			keys = append(keys, "s save as new")
		}
	}
	keys = append(keys, "esc back", "q quit")
	b.WriteString("\n\n" + footer(w, keys...))
	return b.String()
}

// viewSaveProfile renders the "save as profile" section of the summary.
func (m Model) viewSaveProfile(w int) string {
	if m.mode != modeCustom || m.opts.Profiles == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	indent := col{w: 2}
	if !m.canSaveProfile() {
		b.WriteString(row(w, indent, col{s: warn.Render(profileLimitNotice)}) + "\n")
		return b.String()
	}

	mark := dim.Render("[ ]")
	if m.saveProf {
		mark = accent.Render("[x]")
	}
	// One line that reads as a sentence, with the name highlighted:
	// "Save profile as Profile 1". While renaming, the name becomes the input.
	name := profileNameStyle.Render(safe(m.profileName))
	if m.editField == fieldName {
		m.nameInput.Width = max(w-30, minFlexWidth)
		name = m.nameInput.View()
	}
	var label string
	switch {
	case !m.savingNew() && strings.EqualFold(m.profileName, m.editingProfile):
		label = "Update profile " + name
	case !m.savingNew():
		label = "Update profile " + profileNameStyle.Render(safe(m.editingProfile)) + " as " + name
	case m.editingProfile != "":
		label = "Save as new profile " + name
	default:
		label = "Save profile as " + name
	}
	b.WriteString(row(w, indent, col{w: 4, s: mark}, col{s: label}) + "\n")
	if m.editField == fieldName {
		b.WriteString(row(w, indent, col{w: 4}, col{s: dim.Render("enter accept · esc cancel")}) + "\n")
	}
	if dup := m.duplicateOf(m.includes); dup != "" {
		b.WriteString(row(w, indent, col{w: 4}, col{s: dim.Render(fmt.Sprintf("Same selection as profile %q.", safe(dup)))}) + "\n")
	}
	return b.String()
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func tildify(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+"/"); ok {
		return "~/" + rest
	}
	return p
}

// completeDir completes the last element of a path with existing folders,
// like tab completion in a shell.
func completeDir(in string) string {
	if in == "~" {
		return "~/"
	}
	dir, base := filepath.Split(expandHome(in))
	if dir == "" {
		dir = "."
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		return in
	}
	var matches []string
	for _, de := range des {
		name := de.Name()
		if !de.IsDir() || !strings.HasPrefix(name, base) {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		matches = append(matches, name)
	}
	if len(matches) == 0 {
		return in
	}
	common := matches[0]
	for _, mt := range matches[1:] {
		for !strings.HasPrefix(mt, common) {
			_, size := utf8.DecodeLastRuneInString(common)
			common = common[:len(common)-size]
		}
	}
	if len(matches) == 1 {
		common += "/"
	}
	return strings.TrimSuffix(in, base) + common
}
