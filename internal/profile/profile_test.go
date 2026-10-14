package profile

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "profiles.yaml")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func names(ps []Profile) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	s, path := newStore(t)
	if len(s.List("adb:R58M123")) != 0 {
		t.Fatal("empty store should have no profiles")
	}
	if err := s.Save("adb:R58M123", "SM A515F", Profile{Name: " Fotos ", Include: []string{"DCIM/Camera", "Documents"}}, ""); err != nil {
		t.Fatal(err)
	}

	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ps := s2.List("adb:R58M123")
	if len(ps) != 1 || ps[0].Name != "Fotos" || !reflect.DeepEqual(ps[0].Include, []string{"DCIM/Camera", "Documents"}) || ps[0].Updated.IsZero() {
		t.Errorf("unexpected profiles after reload: %+v", ps)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "version: 2") || !strings.Contains(string(data), "device: SM A515F") {
		t.Errorf("file should be written in version 2:\n%s", data)
	}
}

func TestMigratesVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.yaml")
	v1 := `devices:
    uuid:TEST:
        name: FOTOS
        include:
            - DCIM/Camera
            - Documents
        updated: 2026-10-14T01:00:00Z
`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ps := s.List("uuid:TEST")
	if len(ps) != 1 || ps[0].Name != "Profile 1" || !reflect.DeepEqual(ps[0].Include, []string{"DCIM/Camera", "Documents"}) {
		t.Fatalf("v1 profile not migrated: %+v", ps)
	}

	// Saving rewrites the file in the new format without losing the profile.
	if err := s.Save("uuid:TEST", "FOTOS", Profile{Name: "Music", Include: []string{"Music"}}, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "\n        include:") {
		t.Errorf("legacy fields should be gone after saving:\n%s", data)
	}
	s2, _ := Load(path)
	if got := names(s2.List("uuid:TEST")); !reflect.DeepEqual(got, []string{"Profile 1", "Music"}) {
		t.Errorf("profiles after migration = %v", got)
	}
}

func TestLimitNamesReplaceDelete(t *testing.T) {
	s, _ := newStore(t)
	add := func(name string) error {
		return s.Save("usb:A", "FOTOS", Profile{Name: name, Include: []string{name}}, "")
	}
	for _, n := range []string{"One", "Two", "Three"} {
		if err := add(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := add("Four"); !errors.Is(err, ErrLimit) {
		t.Errorf("4th profile: err = %v, want ErrLimit", err)
	}

	// Replacing renames in place and keeps the order.
	if err := s.Save("usb:A", "FOTOS", Profile{Name: "Uno", Include: []string{"One"}}, "one"); err != nil {
		t.Fatal(err)
	}
	if got := names(s.List("usb:A")); !reflect.DeepEqual(got, []string{"Uno", "Two", "Three"}) {
		t.Errorf("after rename = %v", got)
	}
	if err := s.Save("usb:A", "FOTOS", Profile{Name: "two", Include: []string{"x"}}, "Uno"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("renaming onto another name (case-insensitive): err = %v, want ErrNameTaken", err)
	}

	if err := s.Delete("usb:A", "TWO"); err != nil {
		t.Fatal(err)
	}
	if got := names(s.List("usb:A")); !reflect.DeepEqual(got, []string{"Uno", "Three"}) {
		t.Errorf("after delete = %v", got)
	}
	if err := s.Delete("usb:A", "Two"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: err = %v, want ErrNotFound", err)
	}
	if err := add("Four"); err != nil {
		t.Errorf("there is room again after deleting: %v", err)
	}
}

func TestRejectsBadNames(t *testing.T) {
	s, _ := newStore(t)
	for _, n := range []string{"", "   ", strings.Repeat("x", MaxNameLen+1)} {
		if err := s.Save("usb:A", "", Profile{Name: n, Include: []string{"DCIM"}}, ""); !errors.Is(err, ErrBadName) {
			t.Errorf("name %q: err = %v, want ErrBadName", n, err)
		}
	}
}

func TestSaveKeepsProfilesFromOtherInstances(t *testing.T) {
	_, path := newStore(t)
	a, _ := Load(path)
	b, _ := Load(path)
	if err := a.Save("usb:A", "", Profile{Name: "Photos", Include: []string{"DCIM"}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Save("usb:A", "", Profile{Name: "Music", Include: []string{"Music"}}, ""); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(path)
	if got := names(s.List("usb:A")); !reflect.DeepEqual(got, []string{"Photos", "Music"}) {
		t.Errorf("a second instance must not drop the first one's profile: %v", got)
	}
}

func TestRejectsInvalidFiles(t *testing.T) {
	for name, content := range map[string]string{
		"path outside device": "devices:\n  usb:A:\n    profiles:\n      - {name: X, include: [\"../../etc\"]}\n",
		"duplicate names":     "devices:\n  usb:A:\n    profiles:\n      - {name: X, include: [a]}\n      - {name: x, include: [b]}\n",
		"empty name":          "devices:\n  usb:A:\n    profiles:\n      - {name: \"\", include: [a]}\n",
		"v1 path outside":     "devices:\n  usb:A:\n    include: [\"/etc\"]\n",
	} {
		path := filepath.Join(t.TempDir(), "profiles.yaml")
		os.WriteFile(path, []byte(content), 0o644)
		if _, err := Load(path); err == nil {
			t.Errorf("%s: file must be rejected", name)
		}
	}
}
