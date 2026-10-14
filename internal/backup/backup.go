// Package backup copies a device's selection into its backup folder.
//
// Backups are incremental mirrors: only new or changed files are copied, and
// nothing is ever deleted from the backup, even when it was deleted from the
// device.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexisiba/asdfbkp/internal/dest"
	"github.com/alexisiba/asdfbkp/internal/human"
	"github.com/alexisiba/asdfbkp/internal/source"
)

const (
	// reportEvery limits how often progress is reported while copying.
	reportEvery = 100 * time.Millisecond
	// maxConsecutiveFailures aborts the backup when files keep failing in a
	// row, which usually means the device or the destination went away.
	maxConsecutiveFailures = 10
	// maxErrors is how many file errors the result keeps.
	maxErrors = 100
	// mtimeTolerance absorbs filesystems that store times coarsely: FAT keeps
	// modification times with a 2 second precision.
	mtimeTolerance = 2 * time.Second
	// tmpSuffix marks files being written. They are renamed when complete,
	// so an interrupted copy never leaves a half-written file.
	tmpSuffix = ".asdfbkp-tmp"
)

var (
	ErrNoSpace     = errors.New("not enough free space at the destination")
	ErrTooManyFail = errors.New("too many files failed in a row: is the device still connected?")
)

// Job describes a backup.
type Job struct {
	Source   source.Source
	Includes []string // relative paths to back up, "." for everything
	Dir      string   // device folder: <destination>/<device>
	DeviceID string   // written to the folder's marker file

	// For the log.
	DeviceName string
	Version    string // asdfbkp version
}

// Progress is a snapshot of a running backup.
type Progress struct {
	Scanning bool // still looking for files
	Found    int  // files found so far while scanning

	ToCopy   int // new or changed files
	Copied   int
	UpToDate int // already in the backup, skipped
	Failed   int

	BytesTotal  int64     // bytes of the files to copy
	BytesDone   int64     // bytes processed, including failed files
	BytesCopied int64     // bytes of the files copied successfully
	Current     string    // file being copied
	Started     time.Time // when copying started (after scanning)
}

// FileError is a file that could not be backed up.
type FileError struct {
	Rel string
	Err error
}

// Result is the outcome of a backup.
type Result struct {
	Progress
	Errors   []FileError // up to maxErrors; the log has all of them
	Elapsed  time.Duration
	Canceled bool
	LogPath  string // the log of this backup, if it was written
	LogErr   error  // why the log could not be written completely
}

// Run performs the backup. report receives progress snapshots; it is called
// from Run's goroutine and must not block. A canceled ctx stops the backup
// cleanly: files already copied stay, and Result.Canceled is set.
func Run(ctx context.Context, job Job, report func(Progress)) (Result, error) {
	r := &runner{job: job, report: report, start: time.Now()}
	err := r.run(ctx)
	if errors.Is(err, context.Canceled) {
		r.res.Canceled, err = true, nil
	}
	r.res.Progress = r.p
	r.res.Elapsed = time.Since(r.start)
	if err != nil {
		r.ensureLog() // a stopped backup always explains why
	}
	r.log.close(r.res, err)
	if r.log != nil {
		r.res.LogPath, r.res.LogErr = r.log.path, r.log.err
	}
	return r.res, err
}

type runner struct {
	job        Job
	report     func(Progress)
	start      time.Time
	p          Progress
	res        Result
	lastReport time.Time
	safeDirs   map[string]bool // destination folders already checked
	log        *runLog
}

type pending struct {
	file source.File
	dst  string
}

func (r *runner) run(ctx context.Context) error {
	// The device folder comes first: the log of a backup that fails early
	// (for example for lack of space) is written inside it.
	if err := os.MkdirAll(r.job.Dir, 0o755); err != nil {
		return err
	}
	if err := dest.WriteMarker(r.job.Dir, r.job.DeviceID); err != nil {
		return err
	}

	todo, err := r.scan(ctx)
	if err != nil {
		return err
	}

	if r.p.BytesTotal > 0 {
		free, ok := dest.Free(r.job.Dir)
		if ok && uint64(r.p.BytesTotal) > free {
			return fmt.Errorf("%w: %s to copy, %s free", ErrNoSpace, human.Size(r.p.BytesTotal), human.Size(int64(free)))
		}
	}

	r.p.Started = time.Now()
	r.emit(true)
	failedInRow := 0
	for _, t := range todo {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.p.Current = t.file.Rel
		r.emit(false)
		before := r.p.BytesDone
		if err := r.copyFile(ctx, t); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Count the failed file as processed so progress still ends at 100%.
			r.p.BytesDone = before + t.file.Size
			r.fail(t.file.Rel, err)
			if failedInRow++; failedInRow >= maxConsecutiveFailures {
				return ErrTooManyFail
			}
			continue
		}
		failedInRow = 0
		r.p.Copied++
		r.p.BytesCopied += t.file.Size
	}
	r.p.Current = ""
	r.emit(true)
	return nil
}

// scan lists the selection and keeps the files that are new or changed.
func (r *runner) scan(ctx context.Context) ([]pending, error) {
	r.p.Scanning = true
	r.emit(true)
	seen := map[string]bool{} // hand-edited profiles may overlap
	var todo []pending
	for _, inc := range r.job.Includes {
		err := r.job.Source.Walk(ctx, inc, func(f source.File) error {
			if seen[f.Rel] {
				return nil
			}
			seen[f.Rel] = true
			r.p.Found++
			dst, err := r.destPath(f.Rel)
			if err != nil {
				r.fail(f.Rel, err)
				return nil
			}
			if upToDate(f, dst) {
				r.p.UpToDate++
			} else {
				todo = append(todo, pending{file: f, dst: dst})
				r.p.ToCopy++
				r.p.BytesTotal += f.Size
			}
			r.emit(false)
			return nil
		}, r.fail)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// A missing include (for example a folder deleted from the
			// device since the profile was saved) is not fatal.
			r.fail(inc, err)
		}
	}
	r.p.Scanning = false
	return todo, nil
}

// destPath maps a source path into the backup folder, refusing anything that
// would land outside it.
func (r *runner) destPath(rel string) (string, error) {
	if !source.ValidRel(rel) || rel == "." {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	dst := filepath.Join(r.job.Dir, filepath.FromSlash(rel))
	if back, err := filepath.Rel(r.job.Dir, dst); err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the backup folder", rel)
	}
	return dst, nil
}

func upToDate(f source.File, dst string) bool {
	info, err := os.Lstat(dst)
	if err != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
		return false
	}
	diff := info.ModTime().Sub(f.ModTime)
	return diff < mtimeTolerance && diff > -mtimeTolerance
}

func (r *runner) copyFile(ctx context.Context, t pending) (err error) {
	parent := filepath.Dir(t.dst)
	if err := r.mkdirInside(parent); err != nil {
		return err
	}
	if info, err := os.Lstat(t.dst); err == nil && info.IsDir() {
		return errors.New("a folder with the same name exists in the backup")
	}

	// The temp file is created fresh with O_EXCL, which never follows a
	// symlink left at that name. Leftovers of an interrupted run are removed.
	tmp := filepath.Join(parent, "."+filepath.Base(t.dst)+tmpSuffix)
	os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			out.Close()
			os.Remove(tmp)
		}
	}()

	in, err := r.job.Source.Open(ctx, t.file.Rel)
	if err != nil {
		return err
	}
	n, copyErr := r.copyData(ctx, out, in)
	closeErr := in.Close()
	switch {
	case copyErr != nil:
		return copyErr
	case n != t.file.Size:
		// The file changed while copying, or the device stopped sending.
		return fmt.Errorf("expected %s, got %s: the file changed or could not be read", human.Size(t.file.Size), human.Size(n))
	case closeErr != nil:
		return closeErr
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(tmp, t.file.ModTime, t.file.ModTime); err != nil {
		return err
	}
	return os.Rename(tmp, t.dst)
}

// copyData copies in to out, counting progress and stopping on cancel.
func (r *runner) copyData(ctx context.Context, out io.Writer, in io.Reader) (int64, error) {
	buf := make([]byte, 256*1024)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		k, rerr := in.Read(buf)
		if k > 0 {
			if _, werr := out.Write(buf[:k]); werr != nil {
				return n, friendlyWriteError(werr)
			}
			n += int64(k)
			r.p.BytesDone += int64(k)
			r.emit(false)
		}
		if rerr == io.EOF {
			return n, nil
		}
		if rerr != nil {
			return n, rerr
		}
	}
}

// mkdirInside creates dir one folder at a time from the backup folder down,
// refusing symlinks before creating anything, so a symlink in the backup
// (created by someone else) can never redirect writes outside of it.
func (r *runner) mkdirInside(dir string) error {
	if r.safeDirs == nil {
		r.safeDirs = map[string]bool{}
	}
	if r.safeDirs[dir] {
		return nil
	}
	rel, err := filepath.Rel(r.job.Dir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is outside the backup folder", dir)
	}
	cur := r.job.Dir
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		if r.safeDirs[cur] {
			continue
		}
		info, err := os.Lstat(cur)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(cur, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink != 0:
			return errors.New("the backup contains a symlink here; refusing to write through it")
		case !info.IsDir():
			return errors.New("a file with the name of a folder exists in the backup")
		}
		r.safeDirs[cur] = true
	}
	return nil
}

func friendlyWriteError(err error) error {
	switch {
	case strings.Contains(err.Error(), "file too large"):
		return errors.New("the file is too large for the destination's filesystem (FAT32 allows up to 4 GiB)")
	case strings.Contains(err.Error(), "no space left"):
		return ErrNoSpace
	}
	return err
}

func (r *runner) fail(rel string, err error) {
	r.ensureLog()
	r.log.failure(rel, err)
	r.p.Failed++
	if len(r.res.Errors) < maxErrors {
		r.res.Errors = append(r.res.Errors, FileError{Rel: rel, Err: err})
	}
}

// emit reports progress, at most every reportEvery unless forced.
func (r *runner) emit(force bool) {
	if r.report == nil {
		return
	}
	if now := time.Now(); force || now.Sub(r.lastReport) >= reportEvery {
		r.lastReport = now
		r.report(r.p)
	}
}
