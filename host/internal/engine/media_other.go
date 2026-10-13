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
