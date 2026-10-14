package device

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type lsblkDev struct {
	Name       string     `json:"name"`
	Path       string     `json:"path"`
	Type       string     `json:"type"`
	Size       uint64     `json:"size"`
	RM         bool       `json:"rm"`
	Hotplug    bool       `json:"hotplug"`
	Tran       *string    `json:"tran"`
	Serial     *string    `json:"serial"`
	Model      *string    `json:"model"`
	Vendor     *string    `json:"vendor"`
	Label      *string    `json:"label"`
	UUID       *string    `json:"uuid"`
	FSType     *string    `json:"fstype"`
	Mountpoint *string    `json:"mountpoint"`
	Children   []lsblkDev `json:"children"`
}

// ScanUSB lists the mounted partitions of external drives (USB sticks,
// removable disks) using lsblk.
func ScanUSB(ctx context.Context) ([]Device, error) {
	out, err := exec.CommandContext(ctx, "lsblk", "-J", "-b", "-o",
		"NAME,PATH,TYPE,SIZE,RM,HOTPLUG,TRAN,SERIAL,MODEL,VENDOR,LABEL,UUID,FSTYPE,MOUNTPOINT").Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk: %w", err)
	}
	return parseLsblk(out)
}

func parseLsblk(data []byte) ([]Device, error) {
	var tree struct {
		BlockDevices []lsblkDev `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("lsblk: invalid output: %w", err)
	}

	var devs []Device
	for _, disk := range tree.BlockDevices {
		if disk.Type != "disk" || !isExternal(disk) {
			continue
		}
		// A disk without partitions may hold the filesystem directly.
		parts := disk.Children
		if len(parts) == 0 {
			parts = []lsblkDev{disk}
		}
		for _, p := range parts {
			mp := str(p.Mountpoint)
			if mp == "" || isSystemMount(mp) {
				continue
			}
			devs = append(devs, Device{
				Kind: KindUSB,
				ID:   usbID(disk, p),
				Name: usbName(disk, p),
				Path: mp,
				Size: p.Size,
			})
		}
	}
	return devs, nil
}

// isSystemMount detects system partitions in use, for example when Linux is
// booted from a USB stick: they are never offered as a backup source.
func isSystemMount(mp string) bool {
	switch mp {
	case "[SWAP]", "/", "/boot", "/boot/efi", "/efi", "/home", "/usr", "/var":
		return true
	}
	return false
}

func isExternal(d lsblkDev) bool {
	return str(d.Tran) == "usb" || d.RM || d.Hotplug
}

// usbID combines the hardware serial with the filesystem UUID: the serial
// survives a reformat, the UUID tells partitions apart and covers sticks that
// report no serial.
func usbID(disk, part lsblkDev) string {
	var ids []string
	if s := str(disk.Serial); s != "" {
		ids = append(ids, "serial:"+s)
	}
	if u := str(part.UUID); u != "" {
		ids = append(ids, "uuid:"+u)
	}
	return strings.Join(ids, "|")
}

func usbName(disk, part lsblkDev) string {
	if l := str(part.Label); l != "" {
		return l
	}
	name := strings.TrimSpace(str(disk.Vendor) + " " + str(disk.Model))
	if name == "" {
		name = part.Name
	}
	return name
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
