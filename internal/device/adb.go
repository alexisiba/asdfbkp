package device

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ADBInstalled reports whether the adb tool is on the PATH.
func ADBInstalled() bool {
	_, err := exec.LookPath("adb")
	return err == nil
}

// ScanADB lists the phones visible to ADB, over cable or wireless debugging.
func ScanADB(ctx context.Context) ([]Device, error) {
	out, err := exec.CommandContext(ctx, "adb", "devices", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("adb: %w", err)
	}
	devs := parseADBDevices(out)
	for i := range devs {
		if devs[i].Ready() {
			devs[i].ID = hardwareID(ctx, devs[i])
		}
	}
	return devs, nil
}

// hardwareID uses the phone's hardware serial so the profile is the same over
// cable and over wireless debugging, which have different ADB serials.
func hardwareID(ctx context.Context, d Device) string {
	out, err := exec.CommandContext(ctx, "adb", "-s", d.Path, "shell", "getprop", "ro.serialno").Output()
	if s := strings.TrimSpace(string(out)); err == nil && s != "" {
		return "adb:" + s
	}
	return d.ID
}

// parseADBDevices parses lines such as:
//
//	R58M123ABC  device usb:1-1 product:x model:SM_A515F device:a51 transport_id:1
//	192.168.1.5:5555  unauthorized transport_id:2
func parseADBDevices(out []byte) []Device {
	var devs []Device
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] == "List" || strings.HasPrefix(fields[0], "*") {
			continue
		}
		serial, state := fields[0], fields[1]
		name := serial
		for _, f := range fields[2:] {
			if m, ok := strings.CutPrefix(f, "model:"); ok {
				name = strings.ReplaceAll(m, "_", " ")
			}
		}
		devs = append(devs, Device{
			Kind:     KindADB,
			ID:       "adb:" + serial,
			Name:     name,
			Path:     serial,
			State:    state,
			Wireless: isWireless(serial),
		})
	}
	return devs
}

func isWireless(serial string) bool {
	return strings.Contains(serial, ":") || strings.Contains(serial, "._adb-tls-connect.")
}
