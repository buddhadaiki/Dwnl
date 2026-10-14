//go:build windows

package engine

import (
	"dwnl/host/internal/windows"
	"errors"
	win "golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func hasReparsePoint(path string) bool {
	p, err := win.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	attributes, err := win.GetFileAttributes(p)
	return err != nil || attributes&win.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
func diskFull(err error) bool {
	return errors.Is(err, win.ERROR_DISK_FULL) || errors.Is(err, win.ERROR_HANDLE_DISK_FULL)
}

func ensureEngineRoot(path string) error { return windows.CreateDirectories(path, true) }
func removeEngineWork(path, owner string) error {
	return windows.RemoveOwnedTree(path, func() error {
		body, err := os.ReadFile(filepath.Join(path, ".dwnl-engine-work"))
		if err != nil {
			return err
		}
		if string(body) != owner {
			return errors.New("engine temporary ownership mismatch")
		}
		return nil
	})
}
