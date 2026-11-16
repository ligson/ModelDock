//go:build windows

package api

import (
	"golang.org/x/sys/windows"
)

func diskSpace(path string) (total, free uint64) {
	root, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	var freeBytes, totalBytes, totalFree uint64
	if windows.GetDiskFreeSpaceEx(root, &freeBytes, &totalBytes, &totalFree) != nil {
		return 0, 0
	}
	return totalBytes, freeBytes
}
