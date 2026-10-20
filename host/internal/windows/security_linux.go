package windows

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

func UserID() (string, error) { return strconv.Itoa(os.Getuid()), nil }

func protectFD(fd int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Getuid()) {
		return errors.New("private directory must be owned by the current user")
	}
	return unix.Fchmod(fd, 0700)
}

func ProtectDirectory(path string) error {
	fd, err := openPath(path, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return protectFD(fd)
}

func IsWithin(root, path string) bool {
	r, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && r != "." && r != ".." && !strings.HasPrefix(r, "../") && !filepath.IsAbs(r)
}

func DefaultPaths() (stateRoot, destination string, err error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", "", errors.New("absolute user home directory is required")
	}
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		data = filepath.Join(home, ".local", "share")
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(config) {
		config = filepath.Join(home, ".config")
	}
	downloads, err := downloadDirectory(filepath.Join(config, "user-dirs.dirs"), home)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(data, "Dwnl", "state"), filepath.Join(downloads, "Dwnl"), nil
}

// Parse the data file; never source it or expand commands/environment variables.
func downloadDirectory(path, home string) (string, error) {
	fd, err := openPath(path, unix.O_RDONLY|unix.O_NONBLOCK)
	if errors.Is(err, os.ErrNotExist) {
		return filepath.Join(home, "Downloads"), nil
	}
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() || st.Size() > 65536 {
		return "", errors.New("invalid XDG user directory configuration")
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "XDG_DOWNLOAD_DIR" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
			return "", errors.New("invalid XDG download directory")
		}
		value = value[1 : len(value)-1]
		if value == "$HOME" {
			value = home
		} else if strings.HasPrefix(value, "$HOME/") {
			value = filepath.Join(home, strings.TrimPrefix(value, "$HOME/"))
		}
		if strings.ContainsAny(value, "`$\\\x00\r\n\"") || !filepath.IsAbs(value) {
			return "", errors.New("unsupported XDG download directory expression")
		}
		return filepath.Clean(value), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

type Lease struct {
	fd   int
	once sync.Once
}

func AcquireLease(scope string, wait bool) (*Lease, bool, error) {
	// A stable per-UID location coordinates hosts even when XDG paths differ.
	// Never unlink lockfiles: doing so would permit two independently locked inodes.
	dir := fmt.Sprintf("/tmp/dwnl-%d-leases", os.Getuid())
	if err := CreateDirectories(dir, false); err != nil {
		return nil, false, err
	}
	parent, err := openPath(dir, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, false, err
	}
	defer unix.Close(parent)
	var st unix.Stat_t
	if err = unix.Fstat(parent, &st); err != nil {
		return nil, false, err
	}
	if st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 {
		return nil, false, errors.New("lease directory is not user-private")
	}
	name := fmt.Sprintf("%x.lock", sha256.Sum256([]byte(scope)))
	fd, err := unix.Openat(parent, name, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, false, err
	}
	if err = unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, false, err
	}
	if st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		unix.Close(fd)
		return nil, false, errors.New("lease file is not user-private")
	}
	flags := unix.LOCK_EX
	if !wait {
		flags |= unix.LOCK_NB
	}
	for {
		err = unix.Flock(fd, flags)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return &Lease{fd: fd}, false, nil
}

func (l *Lease) Close() {
	if l != nil {
		l.once.Do(func() { unix.Flock(l.fd, unix.LOCK_UN); unix.Close(l.fd) })
	}
}
