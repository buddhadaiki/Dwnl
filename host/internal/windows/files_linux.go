package windows

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// walkPath opens each component relative to the previous descriptor. No
// pathname component, including the last one, is ever followed as a symlink.
func walkPath(path string, allowMissing bool) ([]int, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, errors.New("path must be absolute")
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	fds := []int{fd}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		flags := unix.O_PATH | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		if openErr != nil {
			if allowMissing && errors.Is(openErr, unix.ENOENT) {
				return fds, nil
			}
			closeFDs(fds)
			return nil, &os.PathError{Op: "open", Path: path, Err: openErr}
		}
		var st unix.Stat_t
		if err = unix.Fstat(next, &st); err != nil || st.Mode&unix.S_IFMT == unix.S_IFLNK {
			unix.Close(next)
			closeFDs(fds)
			if err == nil {
				err = errors.New("symlinks are not permitted")
			}
			return nil, err
		}
		fds = append(fds, next)
		fd = next
	}
	return fds, nil
}

func closeFDs(fds []int) {
	for i := len(fds) - 1; i >= 0; i-- {
		unix.Close(fds[i])
	}
}

func CheckPath(path string, allowMissing bool) error {
	fds, err := walkPath(path, allowMissing)
	closeFDs(fds)
	return err
}

// LockPath retains descriptors, but Linux does not prevent renaming/unlinking
// open objects. Operations below use directory-relative descriptors instead.
// Callers using ordinary path APIs must keep their directories user-private.
func LockPath(path string, allowMissing bool) (func(), error) {
	fds, err := walkPath(path, allowMissing)
	if err != nil {
		return nil, err
	}
	return func() { closeFDs(fds) }, nil
}

func openPath(path string, flags int) (int, error) {
	fds, err := walkPath(filepath.Dir(path), false)
	if err != nil {
		return -1, err
	}
	defer closeFDs(fds)
	fd, err := unix.Openat(fds[len(fds)-1], filepath.Base(path), flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT == unix.S_IFLNK {
		unix.Close(fd)
		if err == nil {
			err = errors.New("symlinks are not permitted")
		}
		return -1, err
	}
	return fd, nil
}

func CreateDirectories(path string, private bool) error {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return errors.New("path must be absolute")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { unix.Close(fd) }()
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if part == "" {
			continue
		}
		if err = unix.Mkdirat(fd, part, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return err
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		unix.Close(fd)
		fd = next
	}
	if private {
		return protectFD(fd)
	}
	return nil
}

type FileIdentity struct {
	VolumeSerialNumber uint32
	FileIndexHigh      uint32
	FileIndexLow       uint32
}

func identity(fd int) (FileIdentity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return FileIdentity{}, err
	}
	if uint64(st.Dev) > math.MaxUint32 {
		return FileIdentity{}, errors.New("filesystem device identity exceeds persisted format")
	}
	return FileIdentity{uint32(st.Dev), uint32(st.Ino >> 32), uint32(st.Ino)}, nil
}

func IdentifyFile(path string) (FileIdentity, error) {
	fd, err := openPath(path, unix.O_PATH)
	if err != nil {
		return FileIdentity{}, err
	}
	defer unix.Close(fd)
	return identity(fd)
}

func sameEntry(parent int, name string, want FileIdentity) error {
	fd, err := unix.Openat(parent, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	got, err := identity(fd)
	if err != nil {
		return err
	}
	if got != want {
		return errors.New("owned intermediate was replaced")
	}
	return nil
}

// Linux has no unlink-by-fd primitive. The last identity check and unlinkat
// require a parent protected from concurrent writes by untrusted users; another
// process under the same UID can still race this cleanup operation.
func RemoveExactFile(path string, want FileIdentity) error {
	parent, err := openPath(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, filepath.Base(path), unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("intermediate is not a regular file")
	}
	if err = sameEntry(parent, filepath.Base(path), want); err != nil {
		return err
	}
	return unix.Unlinkat(parent, filepath.Base(path), 0)
}

// PublishExactFile links the pinned inode to a new name without overwriting.
// It uses /proc/self/fd only as the documented unprivileged linkat fallback;
// neither path ever resolves the original source pathname a second time.
func PublishExactFile(source, target string, want, approvedParent FileIdentity) error {
	if !filepath.IsAbs(source) || !filepath.IsAbs(target) || strings.ContainsRune(source+target, 0) {
		return errors.New("publication paths must be absolute")
	}
	source, target = filepath.Clean(source), filepath.Clean(target)
	if filepath.Dir(source) != filepath.Dir(target) || source == target {
		return errors.New("publication requires a new name in the approved parent")
	}
	parent, err := openPath(filepath.Dir(source), unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	actualParent, err := identity(parent)
	if err != nil {
		return err
	}
	if actualParent != approvedParent {
		return errors.New("approved destination was replaced")
	}
	fd, err := unix.Openat(parent, filepath.Base(source), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("intermediate is not a regular file")
	}
	actual, err := identity(fd)
	if err != nil {
		return err
	}
	if actual != want {
		return errors.New("owned intermediate was replaced")
	}
	err = unix.Linkat(fd, "", parent, filepath.Base(target), unix.AT_EMPTY_PATH)
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EINVAL) {
		err = unix.Linkat(unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", fd), parent, filepath.Base(target), unix.AT_SYMLINK_FOLLOW)
	}
	if err != nil {
		return &os.PathError{Op: "publish", Path: target, Err: err}
	}
	// Publication is committed at linkat. A replaced source is preserved. Failure
	// to clean the old name must not turn a committed download into a retry.
	if sameEntry(parent, filepath.Base(source), want) == nil {
		_ = unix.Unlinkat(parent, filepath.Base(source), 0)
	}
	return nil
}

// RemoveOwnedTree stays below opened directory descriptors and never traverses
// a symlink. As with unlinkat above, same-UID concurrent rename is not excluded.
func RemoveOwnedTree(path string, validateOwnership func() error) error {
	if filepath.Clean(path) == "/" {
		return errors.New("cannot remove filesystem root")
	}
	parent, err := openPath(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, filepath.Base(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	want, err := identity(fd)
	if err != nil {
		return err
	}
	if validateOwnership != nil {
		if err = validateOwnership(); err != nil {
			return err
		}
	}
	if err = sameEntry(parent, filepath.Base(path), want); err != nil {
		return err
	}
	if err = validateTree(fd); err != nil {
		return err
	}
	if err = removeChildren(fd); err != nil {
		return err
	}
	if err = sameEntry(parent, filepath.Base(path), want); err != nil {
		return err
	}
	return unix.Unlinkat(parent, filepath.Base(path), unix.AT_REMOVEDIR)
}

func entriesAt(fd int) ([]os.DirEntry, error) {
	copy, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(copy), "owned-directory")
	defer f.Close()
	return f.ReadDir(-1)
}

func validateTree(fd int) error {
	entries, err := entriesAt(fd)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		var st unix.Stat_t
		if err = unix.Fstatat(fd, entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFREG:
		case unix.S_IFDIR:
			child, err := unix.Openat(fd, entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			err = validateTree(child)
			unix.Close(child)
			if err != nil {
				return err
			}
		default:
			return errors.New("owned tree contains a symlink or special file")
		}
	}
	return nil
}

func removeChildren(fd int) error {
	entries, err := entriesAt(fd)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child, err := unix.Openat(fd, entry.Name(), unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		var st unix.Stat_t
		err = unix.Fstat(child, &st)
		want, identityErr := identity(child)
		unix.Close(child)
		if err != nil {
			return err
		}
		if identityErr != nil {
			return identityErr
		}
		flags := 0
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFREG:
		case unix.S_IFDIR:
			child, err = unix.Openat(fd, entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			got, idErr := identity(child)
			if idErr != nil || got != want {
				unix.Close(child)
				return errors.New("owned directory was replaced")
			}
			err = removeChildren(child)
			unix.Close(child)
			if err != nil {
				return err
			}
			flags = unix.AT_REMOVEDIR
		default:
			return errors.New("owned tree contains a symlink or special file")
		}
		if err = sameEntry(fd, entry.Name(), want); err != nil {
			return err
		}
		if err = unix.Unlinkat(fd, entry.Name(), flags); err != nil {
			return err
		}
	}
	return nil
}
