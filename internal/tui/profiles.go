package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/alexisiba/asdfbkp/internal/profile"
)

// profiles returns the current device's saved profiles.
func (m Model) profiles() []profile.Profile {
	if m.opts.Profiles == nil {
		return nil
	}
	return m.opts.Profiles.List(m.dev.ProfileKey())
}

func (m Model) findProfile(name string) (profile.Profile, bool) {
	for _, p := range m.profiles() {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return profile.Profile{}, false
}

func (m Model) atProfileLimit() bool {
	return len(m.profiles()) >= profile.MaxPerDevice
}

// savingNew reports whether saving would add a profile rather than update the
// one being edited.
func (m Model) savingNew() bool {
	return m.editingProfile == "" || m.saveAsNew
}

// canSaveProfile reports whether the summary can offer saving at all.
func (m Model) canSaveProfile() bool {
	return m.mode == modeCustom && m.opts.Profiles != nil && !(m.savingNew() && m.atProfileLimit())
}

// duplicateOf returns the name of another profile with exactly the same
// selection, or "".
func (m Model) duplicateOf(includes []string) string {
	want := slices.Sorted(slices.Values(includes))
	for _, p := range m.profiles() {
		if strings.EqualFold(p.Name, m.editingProfile) && !m.saveAsNew {
			continue
		}
		if slices.Equal(slices.Sorted(slices.Values(p.Include)), want) {
			return p.Name
		}
	}
	return ""
}

// nameTaken reports whether name belongs to another profile of the device.
func (m Model) nameTaken(name string) bool {
	for _, p := range m.profiles() {
		if strings.EqualFold(p.Name, name) && !(m.editingProfile != "" && !m.saveAsNew && strings.EqualFold(p.Name, m.editingProfile)) {
			return true
		}
	}
	return false
}

// defaultName is "Profile N" with the lowest N not used by the device's other
// profiles. Users who want something else rename it with n.
func (m Model) defaultName() string {
	for i := 1; ; i++ {
		if name := fmt.Sprintf("Profile %d", i); !m.nameTaken(name) {
			return name
		}
	}
}

// prepareSave sets the summary's save options for a custom selection.
func (m *Model) prepareSave() {
	if m.mode != modeCustom {
		m.saveProf = false
		return
	}
	if m.editingProfile != "" && !m.saveAsNew {
		if !m.nameEdited {
			m.profileName = m.editingProfile
		}
		m.saveProf = true
		return
	}
	if !m.nameEdited {
		m.profileName = m.defaultName()
	}
	m.saveProf = m.canSaveProfile() && m.duplicateOf(m.includes) == ""
}
