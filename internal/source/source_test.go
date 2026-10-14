package source

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseStat(t *testing.T) {
	out := []byte("regular file|1024|/sdcard/Android/notes.txt\n" +
		"directory|4096|/sdcard/Android/data\n" +
		"directory|4096|/sdcard/Android/media\n")
	es := parseStat("Android", out)
	if len(es) != 3 {
		t.Fatalf("want 3 entries, got %+v", es)
	}
	if es[0].Name != "data" || !es[0].IsDir || !es[0].Restricted {
		t.Errorf("Android/data should be a restricted dir listed first: %+v", es[0])
	}
	if es[1].Name != "media" || es[1].Restricted {
		t.Errorf("unexpected entry: %+v", es[1])
	}
	if es[2].Name != "notes.txt" || es[2].IsDir || es[2].Size != 1024 {
		t.Errorf("unexpected file entry: %+v", es[2])
	}
}

func TestParseStatRejectsUnsafeNames(t *testing.T) {
	out := []byte("directory|0|/sdcard/..\r\n" +
		"directory|0|/sdcard/../../etc\n" +
		"directory|0|/otra/ruta/DCIM\n" +
		"directory|0|/sdcard/DCIM\r\n")
	es := parseStat(".", out)
	if len(es) != 1 || es[0].Name != "DCIM" {
		t.Errorf("only DCIM should survive (and without \\r): %+v", es)
	}
}

func TestValidRel(t *testing.T) {
	for rel, want := range map[string]bool{
		".": true, "DCIM": true, "DCIM/Camera": true,
		"": false, "..": false, "../etc": false, "DCIM/../..": false,
		"/etc": false, "DCIM/": false, "./DCIM": false,
	} {
		if got := ValidRel(rel); got != want {
			t.Errorf("ValidRel(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestParseFind(t *testing.T) {
	out := []byte("2048|1700000000|/sdcard/DCIM/Camera/a.jpg\r\n" +
		"10|1700000001|/sdcard/DCIM/../../etc/passwd\n" +
		"10|1700000001|/sdcard/Music/x.mp3\n" +
		"bad|line\n" +
		"5|1700000002|/sdcard/DCIM/b c.png\n")
	fs := parseFind(adbRoot, "DCIM", out)
	if len(fs) != 2 || fs[0].Rel != "DCIM/Camera/a.jpg" || fs[0].Size != 2048 || fs[0].ModTime.Unix() != 1700000000 || fs[1].Rel != "DCIM/b c.png" {
		t.Errorf("unexpected files: %+v", fs)
	}
	if all := parseFind(adbRoot, ".", out); len(all) != 3 {
		t.Errorf("walking the root should keep every valid file: %+v", all)
	}
}

func TestLocalWalk(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "DCIM", "Camera", "a.jpg"), 100)
	mustWrite(t, filepath.Join(root, "System Volume Information", "x"), 1)
	mustWrite(t, filepath.Join(root, "notes.txt"), 7)
	os.Symlink("/etc", filepath.Join(root, "DCIM", "link"))
	os.Symlink(filepath.Join(root, "notes.txt"), filepath.Join(root, "notes-link.txt"))

	l := &Local{root: root, hide: usbJunk}
	var got []string
	err := l.Walk(context.Background(), ".", func(f File) error {
		got = append(got, f.Rel)
		return nil
	}, func(string, error) {})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"DCIM/Camera/a.jpg", "notes.txt"}) {
		t.Errorf("walk must skip junk and symlinks: %v", got)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/sdcard/My photos/l'été"); got != `'/sdcard/My photos/l'\''été'` {
		t.Errorf("unexpected quoting: %s", got)
	}
}

func TestLocal(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "DCIM", "Camera", "a.jpg"), 100)
	mustWrite(t, filepath.Join(root, "DCIM", "b.jpg"), 50)
	mustWrite(t, filepath.Join(root, "System Volume Information", "x"), 999)
	mustWrite(t, filepath.Join(root, "readme.txt"), 7)

	l := &Local{root: root, hide: usbJunk}
	es, err := l.List(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Name != "DCIM" || es[1].Name != "readme.txt" || es[1].Size != 7 {
		t.Errorf("unexpected root listing: %+v", es)
	}
	if n, _ := l.Size(context.Background(), "DCIM"); n != 150 {
		t.Errorf("DCIM size = %d, want 150", n)
	}
	if n, _ := l.Size(context.Background(), "."); n != 157 {
		t.Errorf("root size = %d, want 157 (junk excluded)", n)
	}
}

func mustWrite(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}
