package backup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexisiba/asdfbkp/internal/dest"
	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/human"
)

// logTimeFormat names log files. It has no ":", which FAT, exFAT and NTFS
// don't allow, and sorts chronologically.
const logTimeFormat = "2006-01-02_15-04-05"

// runLog streams a report of a backup that had problems into
// <device folder>/<device ID>-logs/<timestamp>.log. It is only created at the
// first failure (or when the backup stops), so clean backups leave no log.
// Failures are written as they happen, without the limit of Result.Errors.
// Writing the log can never break the backup: errors are kept in err and
// reported in the result.
type runLog struct {
	w           io.WriteCloser
	path        string
	err         error
	wroteFailed bool
}

// ensureLog creates the log the first time it is needed.
func (r *runner) ensureLog() {
	if r.log != nil {
		return
	}
	l := &runLog{}
	r.log = l
	dir := filepath.Join(r.job.Dir, dest.LogDirName(r.job.DeviceID))
	if l.err = r.mkdirInside(dir); l.err != nil {
		return
	}
	base := r.start.Format(logTimeFormat)
	for i := 1; ; i++ {
		name := base + ".log"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.log", base, i) // two backups in the same second
		}
		// O_EXCL never follows a symlink left at that name.
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) && i < 100 {
			continue
		}
		if err != nil {
			l.err = err
			return
		}
		l.w, l.path = f, f.Name()
		break
	}

	version := r.job.Version
	if version == "" {
		version = "dev"
	}
	l.printf("asdfbkp %s · backup log\n\n", version)
	l.field("Device", fmt.Sprintf("%s (%s)", r.job.DeviceName, r.job.DeviceID))
	l.field("Source", r.job.Source.Root())
	l.field("Destination", r.job.Dir)
	l.field("Selection", strings.Join(r.job.Includes, ", "))
	l.field("Started", r.start.Format("2006-01-02 15:04:05"))
}

func (l *runLog) printf(format string, args ...any) {
	if l == nil || l.w == nil || l.err != nil {
		return
	}
	_, l.err = fmt.Fprintf(l.w, format, args...)
}

// field writes "Label:  value". Values come from the device, so they are
// cleaned: a log shown with cat must not inject terminal escape sequences.
func (l *runLog) field(label, value string) {
	l.printf("%-13s %s\n", label+":", device.Printable(value))
}

func (l *runLog) failure(rel string, err error) {
	if l == nil {
		return
	}
	if !l.wroteFailed {
		l.wroteFailed = true
		l.printf("\nFailed files:\n")
	}
	l.printf("%s: %s\n", device.Printable(rel), device.Printable(err.Error()))
}

// close writes the summary and closes the file.
func (l *runLog) close(res Result, runErr error) {
	if l == nil || l.w == nil {
		return
	}
	l.printf("\n")
	l.field("Finished", fmt.Sprintf("%s (%s)", time.Now().Format("2006-01-02 15:04:05"), res.Elapsed.Round(time.Second)))
	switch {
	case runErr != nil:
		l.field("Result", "stopped: "+runErr.Error())
	case res.Canceled:
		l.field("Result", "canceled by the user")
	case res.Failed > 0:
		noun := "problems"
		if res.Failed == 1 {
			noun = "problem"
		}
		l.field("Result", fmt.Sprintf("finished with %d %s", res.Failed, noun))
	default:
		l.field("Result", "complete")
	}
	l.field("Copied", fmt.Sprintf("%d files (%s)", res.Copied, human.Size(res.BytesCopied)))
	l.field("Up to date", fmt.Sprintf("%d files", res.UpToDate))
	l.field("Failed", fmt.Sprintf("%d files", res.Failed))
	if err := l.w.Close(); err != nil && l.err == nil {
		l.err = err
	}
}
