// Package source reads a device's contents: it lists folders and computes
// sizes, whether the device is reached through the filesystem or over ADB.
//
// Relative paths always use "/" and the root is ".".
package source

import (
	"context"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/alexisiba/asdfbkp/internal/device"
)

// Entry is an item inside a folder.
type Entry struct {
	Name       string
	IsDir      bool
	Size       int64 // files only
	Restricted bool  // never backed up (Android app data, see IsAppData)
}

// Source is the contents of a device.
type Source interface {
	// Root describes the source's root for display.
	Root() string
	// LocalPath returns the path on this machine if the source is local.
	LocalPath() (string, bool)
	List(ctx context.Context, rel string) ([]Entry, error)
	// Size returns the total size in bytes of a file or folder.
	Size(ctx context.Context, rel string) (int64, error)
	// Walk calls fn for every regular file under rel, which can be a file or a
	// folder. Symlinks are never followed. Entries that can't be read are
	// reported to skip and the walk goes on.
	Walk(ctx context.Context, rel string, fn func(File) error, skip func(rel string, err error)) error
	// Open opens a file for reading.
	Open(ctx context.Context, rel string) (io.ReadCloser, error)
}

// File is a regular file found by Walk.
type File struct {
	Rel     string
	Size    int64
	ModTime time.Time
}

// For returns the source for the device.
func For(d device.Device) Source {
	switch d.Kind {
	case device.KindADB:
		return &ADB{serial: d.Path, root: adbRoot}
	case device.KindMTP:
		return &Local{root: d.Path}
	default:
		return &Local{root: d.Path, hide: usbJunk}
	}
}

// IsAppData reports whether rel is, or is inside, the private data of Android
// apps: Android/data and Android/obb. They are never backed up. Over MTP they
// sit one level down, inside the storage folder ("Internal storage/Android/
// data"), and on a phone's SD card they can show up on a USB reader too.
func IsAppData(rel string) bool {
	parts := strings.Split(rel, "/")
	for i := 0; i <= 1 && i+1 < len(parts); i++ {
		if parts[i] == "Android" && (parts[i+1] == "data" || parts[i+1] == "obb") {
			return true
		}
	}
	return false
}

// Within reports whether rel is inside include (or is include itself).
func Within(rel, include string) bool {
	return include == "." || rel == include || strings.HasPrefix(rel, include+"/")
}

// Join joins a relative path and a name.
func Join(rel, name string) string {
	return path.Join(rel, name)
}

// validName rejects names that are not a single path element. A tampered
// device could report ".." to escape the destination folder while copying.
func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}

// ValidRel reports whether rel is a clean relative path that stays inside the
// device root, such as a path read from a hand-edited profile.
func ValidRel(rel string) bool {
	if rel == "." {
		return true
	}
	if rel == "" || path.IsAbs(rel) || path.Clean(rel) != rel || strings.Contains(rel, "\x00") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if !validName(part) {
			return false
		}
	}
	return true
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].IsDir != es[j].IsDir {
			return es[i].IsDir
		}
		return strings.ToLower(es[i].Name) < strings.ToLower(es[j].Name)
	})
}
