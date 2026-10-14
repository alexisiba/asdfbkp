package source

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

// adbRoot is the user's storage. The trailing slash makes find follow the
// /sdcard symlink.
const adbRoot = "/sdcard/"

// ADB is an Android phone accessed over USB or wireless debugging.
type ADB struct {
	serial string
	root   string
}

func (a *ADB) Root() string              { return strings.TrimSuffix(a.root, "/") }
func (a *ADB) LocalPath() (string, bool) { return "", false }

func (a *ADB) path(rel string) string {
	if rel == "." {
		return a.root
	}
	return a.root + rel
}

func (a *ADB) shell(ctx context.Context, cmd string) ([]byte, error) {
	return exec.CommandContext(ctx, "adb", "-s", a.serial, "shell", cmd).Output()
}

func (a *ADB) List(ctx context.Context, rel string) ([]Entry, error) {
	out, err := a.shell(ctx, "find "+shellQuote(a.path(rel))+
		" -mindepth 1 -maxdepth 1 -exec stat -c '%F|%s|%n' {} +")
	// find exits with an error when some entry can't be read, but it still
	// lists the rest.
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("could not list %s: %w", rel, err)
	}
	return parseStat(rel, out), nil
}

// parseStat parses "type|size|path" lines from stat -c '%F|%s|%n'.
func parseStat(rel string, out []byte) []Entry {
	var es []Entry
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		// Android before 7 ends lines with \r\n.
		parts := strings.SplitN(strings.TrimSuffix(sc.Text(), "\r"), "|", 3)
		if len(parts) != 3 {
			continue
		}
		dir, name := path.Split(parts[2])
		if !validName(name) || path.Clean(dir) != path.Clean(adbRoot+rel) {
			continue // name with line breaks or other unexpected output
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		e := Entry{Name: name, IsDir: parts[0] == "directory"}
		if e.IsDir {
			e.Restricted = IsAppData(Join(rel, name))
		} else {
			e.Size = size
		}
		es = append(es, e)
	}
	sortEntries(es)
	return es
}

func (a *ADB) Size(ctx context.Context, rel string) (int64, error) {
	// du runs on the phone: much faster than walking the tree over ADB. Its
	// exit status reports unreadable folders, but the totals are still valid.
	// toybox du can't exclude folders, so app data is measured on its own and
	// subtracted when rel contains it.
	paths := []string{a.path(rel)}
	if rel == "." || rel == "Android" {
		paths = append(paths, a.path("Android/data"), a.path("Android/obb"))
	}
	cmd := "du -sk"
	for _, p := range paths {
		cmd += " " + shellQuote(p)
	}
	out, err := a.shell(ctx, cmd+" 2>/dev/null")
	kb, perr := parseDu(out)
	if perr != nil || len(kb) == 0 {
		if err == nil {
			err = perr
		}
		return 0, fmt.Errorf("could not measure %s: %w", rel, err)
	}
	total := kb[0]
	for _, k := range kb[1:] {
		total -= k
	}
	return max(total, 0) * 1024, nil
}

// parseDu reads the sizes, in KiB, from du -sk output, one line per path.
func parseDu(out []byte) ([]int64, error) {
	var kb []int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		k, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return nil, err
		}
		kb = append(kb, k)
	}
	if len(kb) == 0 {
		return nil, errors.New("du returned nothing")
	}
	return kb, nil
}

func (a *ADB) Walk(ctx context.Context, rel string, fn func(File) error, skip func(string, error)) error {
	// One find on the phone lists every file with its size and modification
	// time, much faster than walking folder by folder over ADB. App data is
	// pruned so find doesn't even walk it; parseFind filters it again.
	out, err := a.shell(ctx, "find "+shellQuote(a.path(rel))+
		" -path '*/Android/data' -prune -o -path '*/Android/obb' -prune -o"+
		" -type f -exec stat -c '%s|%Y|%n' {} + 2>/dev/null")
	if err != nil && len(out) == 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("not found on the device, or it can't be read")
	}
	for _, f := range parseFind(a.root, rel, out) {
		if err := fn(f); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// parseFind parses "size|mtime|path" lines from find ... stat -c '%s|%Y|%n'
// and keeps only valid files inside rel.
func parseFind(root, rel string, out []byte) []File {
	var files []File
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		parts := strings.SplitN(strings.TrimSuffix(sc.Text(), "\r"), "|", 3)
		if len(parts) != 3 {
			continue
		}
		size, err1 := strconv.ParseInt(parts[0], 10, 64)
		mtime, err2 := strconv.ParseInt(parts[1], 10, 64)
		r, ok := strings.CutPrefix(path.Clean(parts[2]), strings.TrimSuffix(root, "/")+"/")
		if err1 != nil || err2 != nil || !ok || !ValidRel(r) || !Within(r, rel) || IsAppData(r) {
			continue // a name with line breaks or other unexpected output
		}
		files = append(files, File{Rel: r, Size: size, ModTime: time.Unix(mtime, 0)})
	}
	return files
}

func (a *ADB) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	// exec-out streams the raw bytes, without the line-ending translation of
	// adb shell. It doesn't report cat's exit status, so the caller must check
	// that it received the expected number of bytes.
	cmd := exec.CommandContext(ctx, "adb", "-s", a.serial, "exec-out", "cat "+shellQuote(a.path(rel))+" 2>/dev/null")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{ReadCloser: out, cmd: cmd}, nil
}

// cmdReader closes the pipe and waits for the command to exit.
type cmdReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (r *cmdReader) Close() error {
	r.ReadCloser.Close()
	return r.cmd.Wait()
}

// shellQuote quotes a path for the phone's shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
