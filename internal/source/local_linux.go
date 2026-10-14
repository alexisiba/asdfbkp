package source

import "syscall"

// deviceOf returns the device ID of the filesystem that holds p.
func deviceOf(p string) (uint64, bool) {
	var st syscall.Stat_t
	if syscall.Lstat(p, &st) != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}
