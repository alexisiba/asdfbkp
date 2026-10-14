package dest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspect(t *testing.T) {
	base := t.TempDir()

	c := Inspect(filepath.Join(base, "Backups", "FOTOS"), "/media/u/FOTOS")
	if len(c.Problems) != 0 || c.Exists || !c.FreeKnown {
		t.Errorf("new folder in a writable dir should be valid: %+v", c)
	}

	if c := Inspect(base, ""); !c.Exists {
		t.Errorf("existing dir should be reported as existing: %+v", c)
	}

	if c := Inspect("/media/u/FOTOS/respaldo/FOTOS", "/media/u/FOTOS"); len(c.Problems) == 0 {
		t.Error("destination inside the source must be rejected")
	}
}

func TestIsInside(t *testing.T) {
	for dir, want := range map[string]bool{
		"/media/u/FOTOS":        true,
		"/media/u/FOTOS/a/b":    true,
		"/media/u/FOTOS2/FOTOS": false,
		"/home/u/Backups/FOTO":  false,
	} {
		if got := isInside(dir, "/media/u/FOTOS"); got != want {
			t.Errorf("isInside(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestSymlinkCannotBypassInsideCheck(t *testing.T) {
	src, base := t.TempDir(), t.TempDir()
	link := filepath.Join(base, "atajo")
	if err := os.Symlink(src, link); err != nil {
		t.Skip(err)
	}
	if c := Inspect(filepath.Join(link, "FOTOS"), src); len(c.Problems) == 0 {
		t.Error("a symlink into the source must still be detected")
	}
}

func TestDeviceDir(t *testing.T) {
	base := t.TempDir()
	if got := DeviceDir(base, "FOTOS", "uuid:A"); got != filepath.Join(base, "FOTOS") {
		t.Fatalf("new device should get its plain name, got %s", got)
	}
	os.Mkdir(filepath.Join(base, "FOTOS"), 0o755)
	WriteMarker(filepath.Join(base, "FOTOS"), "uuid:A")
	if got := DeviceDir(base, "FOTOS", "uuid:A"); got != filepath.Join(base, "FOTOS") {
		t.Errorf("same device must reuse its folder, got %s", got)
	}
	if got := DeviceDir(base, "FOTOS", "uuid:B"); got != filepath.Join(base, "FOTOS-2") {
		t.Errorf("another device with the same name must not share the folder, got %s", got)
	}
	// A non-empty user folder without a marker is left alone.
	os.MkdirAll(filepath.Join(base, "Music", "my-stuff"), 0o755)
	if got := DeviceDir(base, "Music", "uuid:C"); got != filepath.Join(base, "Music-2") {
		t.Errorf("a non-empty user folder must not be reused, got %s", got)
	}
}

func TestLogDirName(t *testing.T) {
	for id, want := range map[string]string{
		"adb:R58M123ABC":            "adb-R58M123ABC-logs",
		"serial:ABC|uuid:1234-ABCD": "serial-ABC-uuid-1234-ABCD-logs",
		"mtp:Xiaomi Redmi Note":     "mtp-Xiaomi-Redmi-Note-logs",
		"":                          "device-logs",
	} {
		if got := LogDirName(id); got != want {
			t.Errorf("LogDirName(%q) = %q, want %q", id, got, want)
		}
	}
}
