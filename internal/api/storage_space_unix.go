//go:build !windows

package api

import "syscall"

func diskSpace(path string) (total, free uint64) {
	var s syscall.Statfs_t
	if syscall.Statfs(path, &s) != nil {
		return 0, 0
	}
	return s.Blocks * uint64(s.Bsize), s.Bavail * uint64(s.Bsize)
}
