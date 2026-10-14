package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexisiba/asdfbkp/internal/dest"
	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/source"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// usb returns a fake USB drive with a few files and a job backing it up.
func usb(t *testing.T, includes ...string) (string, Job) {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "DCIM", "Camera", "a.jpg"), "photo-a")
	write(t, filepath.Join(root, "DCIM", "Camera", "b.jpg"), "photo-b")
	write(t, filepath.Join(root, "Documents", "cv.pdf"), "cv")
	write(t, filepath.Join(root, "Music", "x.mp3"), "music")
	src := source.For(device.Device{Kind: device.KindUSB, Path: root})
	return root, Job{Source: src, Includes: includes, Dir: filepath.Join(t.TempDir(), "FOTOS"), DeviceID: "uuid:TEST"}
}

func run(t *testing.T, job Job) Result {
	t.Helper()
	res, err := Run(context.Background(), job, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCopiesSelectionPreservingPathsAndTimes(t *testing.T) {
	root, job := usb(t, "DCIM/Camera", "Documents")
	old := time.Date(2020, 5, 1, 12, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(root, "Documents", "cv.pdf"), old, old)

	res := run(t, job)
	if res.Copied != 3 || res.Failed != 0 || res.BytesDone != res.BytesTotal || res.BytesCopied != res.BytesTotal {
		t.Fatalf("unexpected result: %+v", res)
	}
	if got := read(t, filepath.Join(job.Dir, "DCIM", "Camera", "a.jpg")); got != "photo-a" {
		t.Errorf("content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(job.Dir, "Music")); !os.IsNotExist(err) {
		t.Error("unselected folders must not be copied")
	}
	info, _ := os.Stat(filepath.Join(job.Dir, "Documents", "cv.pdf"))
	if !info.ModTime().Equal(old) {
		t.Errorf("modification time not preserved: %v", info.ModTime())
	}
	if got := strings.TrimSpace(read(t, filepath.Join(job.Dir, dest.MarkerFile))); got != "uuid:TEST" {
		t.Errorf("marker = %q", got)
	}
}

func TestIncrementalAndNeverDeletes(t *testing.T) {
	root, job := usb(t, ".")
	run(t, job)

	// Nothing changed: everything is up to date.
	if res := run(t, job); res.Copied != 0 || res.UpToDate != 4 {
		t.Fatalf("second run should copy nothing: %+v", res)
	}

	// New, changed and deleted files.
	write(t, filepath.Join(root, "DCIM", "Camera", "c.jpg"), "photo-c")
	write(t, filepath.Join(root, "Documents", "cv.pdf"), "cv version 2")
	os.Remove(filepath.Join(root, "Music", "x.mp3"))

	res := run(t, job)
	if res.Copied != 2 || res.UpToDate != 2 {
		t.Fatalf("only new and changed files should be copied: %+v", res)
	}
	if got := read(t, filepath.Join(job.Dir, "Documents", "cv.pdf")); got != "cv version 2" {
		t.Errorf("changed file not updated: %q", got)
	}
	if got := read(t, filepath.Join(job.Dir, "Music", "x.mp3")); got != "music" {
		t.Error("a file deleted from the device must stay in the backup")
	}
}

func TestFullBackupSkipsAndroidAppData(t *testing.T) {
	root, job := usb(t, ".")
	write(t, filepath.Join(root, "Android", "data", "com.app", "secret.db"), "private")
	write(t, filepath.Join(root, "Android", "media", "com.whatsapp", "a.jpg"), "media")
	if res := run(t, job); res.Copied != 5 {
		t.Fatalf("expected the 4 base files plus Android/media: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(job.Dir, "Android", "data")); !os.IsNotExist(err) {
		t.Error("Android/data must never be backed up")
	}
}

func TestNoTempFilesLeftBehind(t *testing.T) {
	_, job := usb(t, ".")
	run(t, job)
	filepath.WalkDir(job.Dir, func(p string, d os.DirEntry, err error) error {
		if strings.HasSuffix(p, tmpSuffix) {
			t.Errorf("temp file left behind: %s", p)
		}
		return nil
	})
}

func TestSymlinkInBackupCannotRedirectWrites(t *testing.T) {
	_, job := usb(t, "DCIM")
	outside := t.TempDir()
	os.MkdirAll(job.Dir, 0o755)
	if err := os.Symlink(outside, filepath.Join(job.Dir, "DCIM")); err != nil {
		t.Skip(err)
	}
	res := run(t, job)
	if res.Copied != 0 || res.Failed != 2 {
		t.Errorf("writes through a symlink must fail: %+v", res)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("files were written outside the backup: %v", entries)
	}
}

func TestTempSymlinkIsNotFollowed(t *testing.T) {
	_, job := usb(t, "Documents")
	victim := filepath.Join(t.TempDir(), "important.txt")
	write(t, victim, "do not touch")
	os.MkdirAll(filepath.Join(job.Dir, "Documents"), 0o755)
	os.Symlink(victim, filepath.Join(job.Dir, "Documents", ".cv.pdf"+tmpSuffix))

	if res := run(t, job); res.Copied != 1 {
		t.Fatalf("copy should still succeed: %+v", res)
	}
	if got := read(t, victim); got != "do not touch" {
		t.Errorf("a symlink at the temp name was followed: %q", got)
	}
}

func TestCancel(t *testing.T) {
	_, job := usb(t, ".")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Run(ctx, job, nil)
	if err != nil || !res.Canceled {
		t.Errorf("a canceled backup is not an error: res=%+v err=%v", res, err)
	}
}

func TestReportsProgress(t *testing.T) {
	_, job := usb(t, ".")
	var last Progress
	sawScan := false
	_, err := Run(context.Background(), job, func(p Progress) {
		sawScan = sawScan || p.Scanning
		last = p
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawScan || last.Scanning || last.Copied != 4 || last.BytesDone != last.BytesTotal || last.Started.IsZero() {
		t.Errorf("unexpected final progress: %+v (saw scan: %v)", last, sawScan)
	}
}

// flaky wraps a source to simulate read problems.
type flaky struct {
	source.Source
	open func(rel string) (io.ReadCloser, error)
}

func (f flaky) Open(ctx context.Context, rel string) (io.ReadCloser, error) { return f.open(rel) }

func TestShortReadIsAFailureNotACorruptFile(t *testing.T) {
	_, job := usb(t, "Documents")
	job.Source = flaky{Source: job.Source, open: func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("c")), nil // 1 byte instead of 2
	}}
	res := run(t, job)
	if res.Failed != 1 || res.Copied != 0 || res.BytesDone != res.BytesTotal || res.BytesCopied != 0 {
		t.Errorf("unexpected result: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(job.Dir, "Documents", "cv.pdf")); !os.IsNotExist(err) {
		t.Error("an incomplete file must not appear in the backup")
	}
}

func TestAbortsWhenEverythingFails(t *testing.T) {
	root, job := usb(t, ".")
	for i := 0; i < 20; i++ {
		write(t, filepath.Join(root, "many", strings.Repeat("f", i+1)), "x")
	}
	job.Source = flaky{Source: job.Source, open: func(string) (io.ReadCloser, error) {
		return nil, errors.New("device disconnected")
	}}
	res, err := Run(context.Background(), job, nil)
	if !errors.Is(err, ErrTooManyFail) || res.Failed != maxConsecutiveFailures {
		t.Errorf("expected abort after %d failures: res=%+v err=%v", maxConsecutiveFailures, res, err)
	}
	if log := readLog(t, res); !strings.Contains(log, "Result:       stopped: too many files failed in a row") {
		t.Errorf("a stopped backup must explain why in the log:\n%s", log)
	}
}

func readLog(t *testing.T, res Result) string {
	t.Helper()
	if res.LogErr != nil || res.LogPath == "" {
		t.Fatalf("log not written: path=%q err=%v", res.LogPath, res.LogErr)
	}
	return read(t, res.LogPath)
}

func TestCleanBackupLeavesNoLog(t *testing.T) {
	_, job := usb(t, ".")
	if res := run(t, job); res.LogPath != "" || res.LogErr != nil {
		t.Errorf("a clean backup must not write a log: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(job.Dir, dest.LogDirName(job.DeviceID))); !os.IsNotExist(err) {
		t.Error("a clean backup must not create the logs folder")
	}
}

func TestWritesALogWhenFilesFail(t *testing.T) {
	root, job := usb(t, "DCIM", "Documents")
	job.DeviceName, job.Version = "Pixel 8", "0.0.1"
	job.DeviceID = "adb:R58M123ABC"
	job.Source = flaky{Source: job.Source, open: func(rel string) (io.ReadCloser, error) {
		if rel == "Documents/cv.pdf" {
			return nil, errors.New("permission denied")
		}
		return os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	}}

	res := run(t, job)
	if want := filepath.Join(job.Dir, "adb-R58M123ABC-logs"); filepath.Dir(res.LogPath) != want {
		t.Errorf("log folder = %s, want %s", filepath.Dir(res.LogPath), want)
	}
	if name := filepath.Base(res.LogPath); strings.Contains(name, ":") || !strings.HasSuffix(name, ".log") {
		t.Errorf("log name %q must be a timestamp without colons", name)
	}
	log := readLog(t, res)
	for _, want := range []string{"asdfbkp 0.0.1 · backup log", "Pixel 8 (adb:R58M123ABC)", "Selection:    DCIM, Documents",
		"Failed files:\nDocuments/cv.pdf: permission denied", "Result:       finished with 1 problem", "Copied:       2 files"} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}

	// A second backup in the same second gets its own log.
	if res2 := run(t, job); res2.LogPath == res.LogPath {
		t.Error("each backup with problems must have its own log")
	}
}

func TestLogListsEveryFailure(t *testing.T) {
	root, job := usb(t, ".")
	for i := 0; i < maxErrors+50; i++ {
		write(t, filepath.Join(root, "bad", fmt.Sprintf("f%03d", i)), "x")
	}
	// Failures are interleaved with successes so the backup is not aborted.
	n := 0
	job.Source = flaky{Source: job.Source, open: func(rel string) (io.ReadCloser, error) {
		if strings.HasPrefix(rel, "bad/") {
			if n++; n%5 != 0 {
				return nil, errors.New("permission denied\x1b[2J")
			}
		}
		return os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	}}
	res := run(t, job)
	if len(res.Errors) != maxErrors || res.Failed <= maxErrors {
		t.Fatalf("result keeps %d errors of %d failures", len(res.Errors), res.Failed)
	}
	log := readLog(t, res)
	if got := strings.Count(log, ": permission denied"); got != res.Failed {
		t.Errorf("log lists %d failures, want all %d", got, res.Failed)
	}
	if strings.Contains(log, "\x1b") {
		t.Error("escape sequences from error messages must be cleaned in the log")
	}
	if !strings.Contains(log, fmt.Sprintf("Result:       finished with %d problems", res.Failed)) {
		t.Errorf("summary missing:\n%s", log[len(log)-300:])
	}
}

func TestCanceledBackupWithoutFailuresLeavesNoLog(t *testing.T) {
	_, job := usb(t, ".")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res, _ := Run(ctx, job, nil); res.LogPath != "" {
		t.Errorf("canceling is not a failure, no log expected: %s", res.LogPath)
	}
}

func TestLogProblemsNeverBreakTheBackup(t *testing.T) {
	root, job := usb(t, "DCIM", "Documents")
	job.Source = flaky{Source: job.Source, open: func(rel string) (io.ReadCloser, error) {
		if rel == "Documents/cv.pdf" {
			return nil, errors.New("permission denied")
		}
		return os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	}}
	os.MkdirAll(job.Dir, 0o755)
	// A file where the logs folder should be: the log can't be created.
	write(t, filepath.Join(job.Dir, "uuid-TEST-logs"), "in the way")
	res := run(t, job)
	if res.Copied != 2 || res.LogErr == nil {
		t.Errorf("backup must succeed and report the log problem: %+v", res)
	}
}
