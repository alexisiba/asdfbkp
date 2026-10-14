package source

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// usbJunk are folders operating systems create at the root of drives that are
// not worth backing up.
var usbJunk = map[string]bool{
	"System Volume Information": true,
	"$RECYCLE.BIN":              true,
	"lost+found":                true,
	".Trash-1000":               true,
	".Trashes":                  true,
	".Spotlight-V100":           true,
	".fseventsd":                true,
}

// Local is a source reachable through the filesystem: a mounted USB drive or
// a phone mounted over MTP.
type Local struct {
	root string
	hide map[string]bool // names hidden at the root
}

func (l *Local) Root() string              { return l.root }
func (l *Local) LocalPath() (string, bool) { return l.root, true }

func (l *Local) abs(rel string) string {
	return filepath.Join(l.root, filepath.FromSlash(rel))
}

func (l *Local) List(ctx context.Context, rel string) ([]Entry, error) {
	des, err := os.ReadDir(l.abs(rel))
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		if !validName(de.Name()) || rel == "." && l.hide[de.Name()] {
			continue
		}
		e := Entry{Name: de.Name(), IsDir: de.IsDir()}
		if e.IsDir {
			e.Restricted = IsAppData(Join(rel, de.Name()))
		} else {
			if info, err := de.Info(); err == nil {
				e.Size = info.Size()
			}
		}
		out = append(out, e)
	}
	sortEntries(out)
	return out, nil
}

func (l *Local) Walk(ctx context.Context, rel string, fn func(File) error, skip func(string, error)) error {
	start := l.abs(rel)
	rootDev, devOK := deviceOf(start)
	return filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, rerr := filepath.Rel(l.root, p)
		if rerr != nil {
			return rerr
		}
		r = filepath.ToSlash(r)
		if err != nil {
			if p == start {
				return err
			}
			skip(r, err)
			return nil
		}
		if !ValidRel(r) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if filepath.Dir(p) == l.root && l.hide[d.Name()] || IsAppData(r) {
				return filepath.SkipDir
			}
			if dev, ok := deviceOf(p); devOK && ok && dev != rootDev {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks, devices, sockets...
		}
		info, err := d.Info()
		if err != nil {
			skip(r, err)
			return nil
		}
		return fn(File{Rel: r, Size: info.Size(), ModTime: info.ModTime()})
	})
}

func (l *Local) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	return os.Open(l.abs(rel))
}

func (l *Local) Size(ctx context.Context, rel string) (int64, error) {
	start := l.abs(rel)
	rootDev, devOK := deviceOf(start)
	var total int64
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == start {
				return err
			}
			return nil // unreadable folders are skipped
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			r, _ := filepath.Rel(l.root, p)
			if filepath.Dir(p) == l.root && l.hide[d.Name()] || IsAppData(filepath.ToSlash(r)) {
				return filepath.SkipDir
			}
			// Don't cross into other filesystems mounted inside the source:
			// they are not part of the device and could form cycles.
			if dev, ok := deviceOf(p); devOK && ok && dev != rootDev {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
