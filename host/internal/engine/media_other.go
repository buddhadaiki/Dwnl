//go:build !windows

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func hasReparsePoint(path string) bool {
	info, err := os.Lstat(path)
	return err != nil || info.Mode()&os.ModeSymlink != 0
}
func diskFull(err error) bool            { return errors.Is(err, syscall.ENOSPC) }
func ensureEngineRoot(path string) error { return os.MkdirAll(path, 0700) }
func removeEngineWork(path, owner string) error {
	body, err := os.ReadFile(filepath.Join(path, ".dwnl-engine-work"))
	if err != nil || string(body) != owner {
		return errors.New("engine temporary ownership mismatch")
	}
	return os.RemoveAll(path)
}
