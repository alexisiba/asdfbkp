// Package dest validates the folder a backup is saved to.
package dest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Check is the result of inspecting a destination.
type Check struct {
	Dir       string // final backup folder: <destination>/<device>
	Exists    bool
	Free      uint64
	FreeKnown bool
	// Problems prevent the backup from starting.
	Problems []string
}

// Inspect checks the folder dir. source is the local path of the source, or ""
// when the source is not local (ADB).
func Inspect(dir, source string) Check {
	c := Check{Dir: dir}

	if source != "" && isInside(resolve(dir), resolve(source)) {
		c.Problems = append(c.Problems, "The destination is inside the device you want to back up.")
		return c
	}

	existing := dir
	for {
		info, err := os.Stat(existing)
		if err == nil {
			if !info.IsDir() {
				c.Problems = append(c.Problems, existing+" is not a folder.")
				return c
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			c.Problems = append(c.Problems, err.Error())
			return c
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	c.Exists = existing == dir

	if syscall.Access(existing, 2 /* W_OK */) != nil {
		c.Problems = append(c.Problems, "You don't have write permission in "+existing+".")
	}

	c.Free, c.FreeKnown = Free(existing)
	return c
}

// Free returns the space available to the user on the filesystem holding p.
func Free(p string) (uint64, bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(p, &st) != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}

// resolve follows the symlinks in the part of p that already exists, so a
// symlink cannot be used to bypass the isInside check.
func resolve(p string) string {
	rest := ""
	for cur := p; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func isInside(dir, root string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
