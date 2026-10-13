//go:build windows

package engine

import (
	"errors"
	"golang.org/x/sys/windows"
)

func hasReparsePoint(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	attributes, err := windows.GetFileAttributes(p)
	return err != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
func diskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}
