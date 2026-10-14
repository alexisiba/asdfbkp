package device

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanMTP looks for phones mounted over MTP through gvfs
// (/run/user/<uid>/gvfs/mtp:host=...). It is the fallback when ADB is missing.
func ScanMTP() ([]Device, error) {
	dir := filepath.Join("/run/user", fmt.Sprint(os.Getuid()), "gvfs")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gvfs: %w", err)
	}

	var devs []Device
	for _, e := range entries {
		host, ok := strings.CutPrefix(e.Name(), "mtp:host=")
		if !ok {
			continue
		}
		devs = append(devs, Device{
			Kind: KindMTP,
			Name: strings.ReplaceAll(host, "_", " "),
			Path: filepath.Join(dir, e.Name()),
		})
	}
	return devs, nil
}

// dedupeMTP drops MTP mounts of phones that are already available over ADB.
// gvfs usually includes the hardware serial in the host name.
func dedupeMTP(mtp, adb []Device) []Device {
	var out []Device
next:
	for _, m := range mtp {
		for _, a := range adb {
			serial := strings.TrimPrefix(a.ID, "adb:")
			if a.Ready() && strings.Contains(filepath.Base(m.Path), serial) {
				continue next
			}
		}
		out = append(out, m)
	}
	return out
}
