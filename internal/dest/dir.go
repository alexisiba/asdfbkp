package dest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MarkerFile identifies the device that owns a backup folder.
const MarkerFile = ".asdfbkp-id"

const maxSuffix = 99

// DeviceDir picks the device's folder inside base. If <base>/<name> already
// belongs to another device (two USB sticks with the same label, two phones of
// the same model) or is a non-empty folder of the user, it uses <name>-2,
// <name>-3... so backups never get mixed.
func DeviceDir(base, name, id string) string {
	for i := 1; i <= maxSuffix; i++ {
		dir := filepath.Join(base, name)
		if i > 1 {
			dir = fmt.Sprintf("%s-%d", dir, i)
		}
		if belongsTo(dir, id) {
			return dir
		}
	}
	return filepath.Join(base, fmt.Sprintf("%s-%d", name, time.Now().Unix()))
}

// belongsTo reports whether dir can be used for device id: it doesn't exist,
// it is empty, or it carries that device's marker.
func belongsTo(dir, id string) bool {
	data, err := os.ReadFile(filepath.Join(dir, MarkerFile))
	if err == nil {
		return strings.TrimSpace(string(data)) == id
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return err == nil && len(entries) == 0
}

// WriteMarker marks dir as the backup of device id.
func WriteMarker(dir, id string) error {
	return os.WriteFile(filepath.Join(dir, MarkerFile), []byte(id+"\n"), 0o644)
}

// LogDirName is the name of the folder, inside the device folder, that keeps
// one log per backup: the device ID plus "-logs", such as
// "adb-R58M123ABC-logs". The ID makes it unique, so it can't clash with a
// folder of the device itself. Characters not allowed on FAT, exFAT or NTFS
// (":", "|"...) become "-".
func LogDirName(deviceID string) string {
	var b strings.Builder
	for _, r := range deviceID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-.")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if name == "" {
		name = "device"
	}
	return name + "-logs"
}
