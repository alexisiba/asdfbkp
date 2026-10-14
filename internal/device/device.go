// Package device detects the devices asdfbkp can back up.
package device

import (
	"context"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Kind tells how the device is accessed.
type Kind int

const (
	KindUSB Kind = iota // mounted mass storage drive
	KindADB             // Android over USB or wireless debugging (preferred)
	KindMTP             // Android over MTP (slow fallback)
)

func (k Kind) String() string {
	switch k {
	case KindUSB:
		return "USB"
	case KindADB:
		return "Android (ADB)"
	case KindMTP:
		return "Android (MTP)"
	}
	return "unknown"
}

// Device is a detected device.
type Device struct {
	Kind Kind
	// ID identifies the device stably so a profile can be attached to it:
	// hardware serial + UUID for USB, hardware serial for Android.
	// Empty when there is no reliable identifier (MTP).
	ID    string
	Name  string // human-readable name
	Path  string // mount point (USB/MTP) or ADB serial
	Size  uint64 // bytes; 0 if unknown
	State string // ADB only: "device", "unauthorized", "offline"...
	// Wireless reports an ADB connection over wireless debugging.
	Wireless bool
}

// ProfileKey is the key the device's profile is stored under. Without a
// reliable identifier (MTP, USB with no serial or UUID) its name is used.
func (d Device) ProfileKey() string {
	if d.ID != "" {
		return d.ID
	}
	if d.Kind == KindMTP {
		return "mtp:" + d.Name
	}
	return "name:" + d.Name
}

// FolderName is the name of the device's subfolder inside the destination.
func (d Device) FolderName() string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(d.Name) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
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
		return "device"
	}
	return name
}

// Ready reports whether the device can be backed up right now.
func (d Device) Ready() bool {
	return d.Kind != KindADB || d.State == "device"
}

// Result gathers what a scan found.
type Result struct {
	Devices      []Device
	ADBAvailable bool
	Errors       []error
}

// Scan runs every detector in parallel.
func Scan(ctx context.Context) Result {
	var (
		wg       sync.WaitGroup
		usb, mtp []Device
		adb      []Device
		usbErr   error
		adbErr   error
		mtpErr   error
		hasADB   = ADBInstalled()
	)

	wg.Add(2)
	go func() { defer wg.Done(); usb, usbErr = ScanUSB(ctx) }()
	go func() { defer wg.Done(); mtp, mtpErr = ScanMTP() }()
	if hasADB {
		wg.Add(1)
		go func() { defer wg.Done(); adb, adbErr = ScanADB(ctx) }()
	}
	wg.Wait()

	res := Result{ADBAvailable: hasADB}
	for _, err := range []error{usbErr, adbErr, mtpErr} {
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
	}

	res.Devices = append(res.Devices, adb...)
	res.Devices = append(res.Devices, dedupeMTP(mtp, adb)...)
	res.Devices = append(res.Devices, usb...)
	for i := range res.Devices {
		res.Devices[i].Name = Printable(res.Devices[i].Name)
	}
	return res
}

// Printable replaces control and text-direction characters with "?". Names
// come from the device (labels, models, file names); printed as-is they could
// inject terminal escape sequences or disguise a file name.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) || r == utf8.RuneError {
			return '?'
		}
		return r
	}, s)
}
