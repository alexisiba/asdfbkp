// Package profile stores what to back up from each device. A profile never
// stores the destination: that is chosen every time asdfbkp runs.
package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/source"
)

const (
	// MaxPerDevice is how many profiles a device can have. Raising it later is
	// free; lowering it would strand profiles users already saved.
	MaxPerDevice = 3
	// MaxNameLen is the longest profile name, in characters.
	MaxNameLen = 32

	currentVersion = 2
	// legacyName is given to the single profile of a version 1 file.
	legacyName = "Profile 1"
)

var (
	ErrLimit     = fmt.Errorf("a device can have at most %d profiles", MaxPerDevice)
	ErrNameTaken = errors.New("a profile with that name already exists")
	ErrNotFound  = errors.New("profile not found")
	ErrBadName   = fmt.Errorf("a profile name must be 1 to %d characters", MaxNameLen)
)

// Profile is a named selection of what to back up from a device.
type Profile struct {
	Name string `yaml:"name"`
	// Include holds the relative paths to back up in full, with "/" as the
	// separator. "." is the whole device.
	Include []string  `yaml:"include,flow"`
	Updated time.Time `yaml:"updated"`
}

// Device holds the profiles of one device.
type Device struct {
	Device   string    `yaml:"device,omitempty"` // human-readable name, for reference
	Profiles []Profile `yaml:"profiles"`

	// Version 1 kept a single profile directly on the device. These fields
	// are only read, to migrate old files.
	LegacyName    string    `yaml:"name,omitempty"`
	LegacyInclude []string  `yaml:"include,omitempty"`
	LegacyUpdated time.Time `yaml:"updated,omitempty"`
}

// Store is the profiles file.
type Store struct {
	path    string
	Version int               `yaml:"version"`
	Devices map[string]Device `yaml:"devices"`
}

// DefaultPath is ~/.config/asdfbkp/profiles.yaml.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "asdfbkp", "profiles.yaml"), nil
}

// CleanName normalizes a profile name typed by the user.
func CleanName(name string) string {
	return strings.TrimSpace(device.Printable(name))
}

func validName(name string) bool {
	return name != "" && name == CleanName(name) && utf8.RuneCountInString(name) <= MaxNameLen
}

// Load reads the profiles file. If it doesn't exist, it returns an empty one.
// Version 1 files are migrated in memory and rewritten on the next save.
func Load(path string) (*Store, error) {
	s := &Store{path: path, Version: currentVersion, Devices: map[string]Device{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Devices == nil {
		s.Devices = map[string]Device{}
	}
	s.Version = currentVersion
	for key, d := range s.Devices {
		d = migrate(d)
		if err := validateDevice(d); err != nil {
			return nil, fmt.Errorf("%s: device %q: %w", path, key, err)
		}
		s.Devices[key] = d
	}
	return s, nil
}

func migrate(d Device) Device {
	if len(d.Profiles) == 0 && len(d.LegacyInclude) > 0 {
		d.Device = d.LegacyName
		d.Profiles = []Profile{{Name: legacyName, Include: d.LegacyInclude, Updated: d.LegacyUpdated}}
	}
	d.LegacyName, d.LegacyInclude, d.LegacyUpdated = "", nil, time.Time{}
	return d
}

func validateDevice(d Device) error {
	for i, p := range d.Profiles {
		if err := validateProfile(p); err != nil {
			return fmt.Errorf("profile %q: %w", p.Name, err)
		}
		for _, q := range d.Profiles[:i] {
			if strings.EqualFold(p.Name, q.Name) {
				return fmt.Errorf("profile %q: %w", p.Name, ErrNameTaken)
			}
		}
	}
	return nil
}

func validateProfile(p Profile) error {
	if !validName(p.Name) {
		return ErrBadName
	}
	if len(p.Include) == 0 {
		return errors.New("nothing to back up")
	}
	for _, rel := range p.Include {
		if !source.ValidRel(rel) {
			return fmt.Errorf("invalid path %q", rel)
		}
	}
	return nil
}

// List returns the device's profiles in creation order.
func (s *Store) List(key string) []Profile {
	return append([]Profile(nil), s.Devices[key].Profiles...)
}

// Save stores profile p for the device. With replace empty it adds a new
// profile; otherwise it replaces the profile named replace, which also allows
// renaming it.
func (s *Store) Save(key, deviceName string, p Profile, replace string) error {
	p.Name = CleanName(p.Name)
	p.Updated = time.Now()
	if err := validateProfile(p); err != nil {
		return err
	}
	return s.update(func(latest *Store) error {
		d := latest.Devices[key]
		d.Device = deviceName
		idx := -1
		if replace != "" {
			if idx = index(d.Profiles, replace); idx < 0 {
				return ErrNotFound
			}
		}
		for i, q := range d.Profiles {
			if i != idx && strings.EqualFold(q.Name, p.Name) {
				return ErrNameTaken
			}
		}
		if idx >= 0 {
			d.Profiles[idx] = p
		} else {
			if len(d.Profiles) >= MaxPerDevice {
				return ErrLimit
			}
			d.Profiles = append(d.Profiles, p)
		}
		latest.Devices[key] = d
		return nil
	})
}

// Delete removes the device's profile with the given name.
func (s *Store) Delete(key, name string) error {
	return s.update(func(latest *Store) error {
		d := latest.Devices[key]
		i := index(d.Profiles, name)
		if i < 0 {
			return ErrNotFound
		}
		d.Profiles = append(d.Profiles[:i], d.Profiles[i+1:]...)
		if len(d.Profiles) == 0 {
			delete(latest.Devices, key)
		} else {
			latest.Devices[key] = d
		}
		return nil
	})
}

func index(ps []Profile, name string) int {
	for i, p := range ps {
		if strings.EqualFold(p.Name, name) {
			return i
		}
	}
	return -1
}

// update re-reads the file, applies change to what it read and writes it
// back, so profiles saved by another asdfbkp instance running at the same time
// are never dropped.
func (s *Store) update(change func(latest *Store) error) error {
	latest, err := Load(s.path)
	if err != nil {
		return err
	}
	if err := change(latest); err != nil {
		return err
	}
	if err := latest.save(); err != nil {
		return err
	}
	s.Devices = latest.Devices
	return nil
}

func (s *Store) save() error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Write to a unique temp file and rename it, so a failure never leaves a
	// half-written file.
	tmp, err := os.CreateTemp(dir, ".profiles-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
