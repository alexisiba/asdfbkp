package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/profile"
)

// run executes a command and feeds its messages to the model, as Bubble Tea
// would, except for spinner ticks.
func run(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(t, m, c)
		}
	case nil:
	default:
		if strings.HasSuffix(reflect.TypeOf(msg).String(), "TickMsg") {
			return m
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		m = run(t, m, next)
	}
	return m
}

func press(t *testing.T, m tea.Model, keys ...string) tea.Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		m = run(t, m, cmd)
	}
	return m
}

// flat joins the view into a single line so assertions don't depend on where
// long text wraps.
func flat(view string) string {
	return strings.Join(strings.Fields(view), " ")
}

func write(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCustomFlowSavesProfile(t *testing.T) {
	usb, out := t.TempDir(), t.TempDir()
	write(t, filepath.Join(usb, "DCIM", "Camera", "a.jpg"), 2048)
	write(t, filepath.Join(usb, "DCIM", "Screenshots", "s.png"), 512)
	write(t, filepath.Join(usb, "Documents", "cv.pdf"), 1024)
	write(t, filepath.Join(usb, "Music", "x.mp3"), 4096)

	store, err := profile.Load(filepath.Join(t.TempDir(), "profiles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dev := device.Device{Kind: device.KindUSB, ID: "uuid:TEST", Name: "FOTOS", Path: usb}
	start := func() tea.Model {
		var m tea.Model = New(Options{Version: "0.0.1", Dest: out, Profiles: store})
		m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		m, _ = m.Update(scanDoneMsg(device.Result{Devices: []device.Device{dev}}))
		return m
	}

	// Device → custom → open DCIM → mark Camera and Documents.
	m := press(t, start(), "enter", "down", "enter")
	if m.(Model).screen != screenTree {
		t.Fatalf("expected tree screen, got %v", m.(Model).screen)
	}
	m = press(t, m, "right", "down", "space", "down", "down", "space")
	view := m.View()
	t.Log("\n" + view)
	for _, want := range []string{"[x]", "[~]", "Camera", "Selected: 3.0 KiB"} {
		if !strings.Contains(view, want) {
			t.Errorf("tree view missing %q", want)
		}
	}

	m = press(t, m, "enter")
	view = flat(m.View())
	t.Log("\n" + m.View())
	for _, want := range []string{"FOTOS/", "(will be created)", "3.0 KiB", "2 items: DCIM/Camera, Documents", "✓", "[x] Save"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary missing %q", want)
		}
	}

	m = press(t, m, "enter")
	if m.(Model).screen != screenDone {
		t.Fatalf("expected result screen, got %v: %s", m.(Model).screen, m.(Model).notice)
	}
	done := flat(m.View())
	t.Log("\n" + m.View())
	for _, want := range []string{"✓ Backup complete", "Copied 2 files · 3.0 KiB", "Up to date 0 files", `Profile "Profile 1" saved.`} {
		if !strings.Contains(done, want) {
			t.Errorf("result screen missing %q", want)
		}
	}
	for _, f := range []string{"DCIM/Camera/a.jpg", "Documents/cv.pdf"} {
		if _, err := os.Stat(filepath.Join(out, "FOTOS", f)); err != nil {
			t.Errorf("%s was not copied: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "FOTOS", "Music")); !os.IsNotExist(err) {
		t.Error("unselected Music was copied")
	}
	ps := store.List("uuid:TEST")
	if len(ps) != 1 || ps[0].Name != "Profile 1" || !reflect.DeepEqual(ps[0].Include, []string{"DCIM/Camera", "Documents"}) {
		t.Fatalf("profile not saved correctly: %+v", ps)
	}

	// Next time the profile is the first option and goes straight to the summary.
	m = press(t, start(), "enter")
	view = m.View()
	if !strings.Contains(view, "› Profile 1") {
		t.Errorf("mode screen should offer the saved profile first:\n%s", view)
	}
	m = press(t, m, "enter")
	if v := flat(m.View()); !strings.Contains(v, "2 items: DCIM/Camera, Documents") || !strings.Contains(v, "Profile Profile 1") {
		t.Errorf("profile summary wrong:\n%s", m.View())
	}
}

func TestMultipleProfiles(t *testing.T) {
	usb := t.TempDir()
	for _, d := range []string{"DCIM", "Documents", "Music", "Pictures"} {
		write(t, filepath.Join(usb, d, "f"), 10)
	}
	store, err := profile.Load(filepath.Join(t.TempDir(), "profiles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dev := device.Device{Kind: device.KindUSB, ID: "uuid:TEST", Name: "FOTOS", Path: usb}
	// start opens asdfbkp and selects the USB drive.
	start := func() tea.Model {
		var m tea.Model = New(Options{Version: "0.0.1", Dest: t.TempDir(), Profiles: store})
		m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		m, _ = m.Update(scanDoneMsg(device.Result{Devices: []device.Device{dev}}))
		return press(t, m, "enter")
	}
	list := func() []string {
		var out []string
		for _, p := range store.List("uuid:TEST") {
			out = append(out, p.Name+"="+strings.Join(p.Include, "+"))
		}
		return out
	}
	expect := func(want ...string) {
		t.Helper()
		if got := list(); !reflect.DeepEqual(got, want) {
			t.Fatalf("profiles = %v, want %v", got, want)
		}
	}
	ready := func(m tea.Model) {
		t.Helper()
		if s := m.(Model).screen; s != screenDone {
			t.Fatalf("expected result screen, got %v (notice %q):\n%s", s, m.(Model).notice, m.View())
		}
	}

	// 1. Custom → Documents → rename the profile to "Docs" with n.
	m := press(t, start(), "down", "enter", "down", "space", "enter")
	if !strings.Contains(flat(m.View()), "[x] Save profile as Profile 1") {
		t.Fatalf("default name should be Profile 1:\n%s", m.View())
	}
	m = press(t, m, "n", "ctrl+u", "Docs", "enter", "enter")
	ready(m)
	expect("Docs=Documents")

	// 2. A second profile keeps the default name: the lowest free number,
	// Profile 1, since "Docs" was renamed. The cursor skips the separator:
	// Docs → Full → Custom.
	m = press(t, start(), "down", "down", "enter", "down", "down", "space", "enter", "enter")
	ready(m)
	expect("Docs=Documents", "Profile 1=Music")

	// 3. Fixed creation order, cursor on the first; e updates that profile.
	m = start()
	if v := m.View(); !strings.Contains(v, "› Docs") || strings.Index(v, "Docs") > strings.Index(v, "Profile 1") {
		t.Fatalf("profiles should be listed in creation order with the cursor first:\n%s", v)
	}
	m = press(t, m, "e", "down", "down", "down", "space", "enter")
	if !strings.Contains(flat(m.View()), "[x] Update profile Docs") {
		t.Fatalf("editing should update the profile:\n%s", m.View())
	}
	m = press(t, m, "enter")
	ready(m)
	expect("Docs=Documents+Pictures", "Profile 1=Music")

	// 4. e then s saves a copy as a new profile. Same selection, so it warns
	// and starts unchecked; p checks it.
	m = press(t, start(), "e", "enter", "s")
	if v := flat(m.View()); !strings.Contains(v, `Same selection as profile "Docs"`) || !strings.Contains(v, "[ ] Save as new profile Profile 2") {
		t.Fatalf("duplicate selection should warn and start unchecked:\n%s", m.View())
	}
	m = press(t, m, "space", "enter")
	ready(m)
	expect("Docs=Documents+Pictures", "Profile 1=Music", "Profile 2=Documents+Pictures")

	// 5. At the limit a new custom selection can't be saved.
	m = press(t, start(), "down", "down", "down", "down", "enter", "space", "enter")
	if !strings.Contains(flat(m.View()), "Profile limit reached (3)") {
		t.Fatalf("limit should be shown:\n%s", m.View())
	}
	m = press(t, m, "enter")
	ready(m)
	expect("Docs=Documents+Pictures", "Profile 1=Music", "Profile 2=Documents+Pictures")

	// 6. x asks first: n keeps the profile, y deletes it.
	m = press(t, start(), "x")
	if !strings.Contains(m.View(), `Delete profile "Docs"? y/n`) {
		t.Fatalf("delete must ask for confirmation:\n%s", m.View())
	}
	m = press(t, m, "n")
	expect("Docs=Documents+Pictures", "Profile 1=Music", "Profile 2=Documents+Pictures")
	m = press(t, m, "x", "y")
	expect("Profile 1=Music", "Profile 2=Documents+Pictures")
	if !strings.Contains(m.View(), "› Profile 1") {
		t.Errorf("cursor should move to the first remaining profile:\n%s", m.View())
	}
}

func TestSummaryBlocksDestinationInsideSource(t *testing.T) {
	usb := t.TempDir()
	write(t, filepath.Join(usb, "a.txt"), 1)
	var m tea.Model = New(Options{Version: "0.0.1", Dest: usb})
	m, _ = m.Update(scanDoneMsg(device.Result{Devices: []device.Device{{Kind: device.KindUSB, Name: "FOTOS", Path: usb}}}))
	m = press(t, m, "enter", "enter", "enter") // device → full → try to start
	if m.(Model).screen != screenSummary || !strings.Contains(m.View(), "inside the device") {
		t.Errorf("backup into the source itself must be blocked:\n%s", m.View())
	}
}

func TestCompleteDir(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{"Backups", "Bakery", "Music", ".hidden"} {
		os.Mkdir(filepath.Join(base, d), 0o755)
	}
	for in, want := range map[string]string{
		base + "/Bac": base + "/Backups/",
		base + "/Ba":  base + "/Ba",
		base + "/B":   base + "/Ba",
		base + "/.h":  base + "/.hidden/",
		base + "/x":   base + "/x",
		base + "/":    base + "/",
	} {
		if got := completeDir(in); got != want {
			t.Errorf("completeDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUntrustedNamesCannotInjectEscapes(t *testing.T) {
	usb := t.TempDir()
	evil := "x\x1b]0;hacked\x07\u202egpj.exe"
	write(t, filepath.Join(usb, evil, "a"), 1)
	var m tea.Model = New(Options{Version: "0.0.1", Dest: t.TempDir()})
	m, _ = m.Update(scanDoneMsg(device.Result{Devices: []device.Device{{Kind: device.KindUSB, Name: "FOTOS", Path: usb}}}))
	m = press(t, m, "enter", "down", "enter")
	view := m.View()
	if strings.Contains(view, "\x1b]0;") || strings.Contains(view, "\u202e") {
		t.Errorf("control characters from file names reached the terminal: %q", view)
	}
	if !strings.Contains(view, "x?]0;hacked??gpj.exe") {
		t.Errorf("sanitized name should still be shown:\n%s", view)
	}
}
