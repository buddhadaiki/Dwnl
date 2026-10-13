//go:build !windows

package engine

import (
	"errors"
	"os"
	"syscall"
)

func hasReparsePoint(path string) bool {
	info, err := os.Lstat(path)
	return err != nil || info.Mode()&os.ModeSymlink != 0
}
func diskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }
func publishNoReplace(from, to string) error {
	if err := os.Link(from, to); err != nil {
		return err
	}
	return os.Remove(from)
}
