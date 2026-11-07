//go:build !windows

package engine

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// fileIdentity names one version of a file: its device and inode, size and
// modification time. Rewriting, replacing or moving another file into its
// place changes the name.
func fileIdentity(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return "", errors.New("not a regular local file")
	}
	return fmt.Sprintf("%x-%x-%x-%d", stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano()), nil
}
