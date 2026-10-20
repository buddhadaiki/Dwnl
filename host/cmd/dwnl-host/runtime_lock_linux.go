package main

import (
	"dwnl/host/internal/windows"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// A shared lifetime lock excludes installer updates/removal. Never unlink the
// inode: a new lockfile would let old and new hosts coordinate different locks.
func acquireRuntimeUse(executable string) (func(), error) {
	parent := filepath.Dir(filepath.Dir(executable))
	if err := windows.CheckPath(parent, false); err != nil {
		return nil, err
	}
	dir, err := unix.Openat2(unix.AT_FDCWD, parent, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, ".dwnl-use.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if st.Uid != uint32(os.Getuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Nlink != 1 {
		unix.Close(fd)
		return nil, errors.New("invalid installation lock")
	}
	if err = unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}
