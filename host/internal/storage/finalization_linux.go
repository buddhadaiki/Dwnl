package storage

import (
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func validLocalPath(path string) bool {
	return privateText(path, 4096) && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}

func linuxPhysicalID(stat *unix.Stat_t) (contracts.PhysicalIdentity, error) {
	if stat.Dev > math.MaxUint32 {
		return contracts.PhysicalIdentity{}, errors.New("device identity exceeds journal format")
	}
	return contracts.PhysicalIdentity{VolumeSerialNumber: uint32(stat.Dev), FileIndexHigh: uint32(stat.Ino >> 32), FileIndexLow: uint32(stat.Ino)}, nil
}

func verifyPreparedFile(proof contracts.PreparedFinalization) error {
	parent := filepath.Dir(proof.File.Path)
	// openat2 rejects symlinks in every component, including concurrent swaps.
	dir, err := unix.Openat2(unix.AT_FDCWD, parent, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	var parentStat unix.Stat_t
	if err = unix.Fstat(dir, &parentStat); err != nil {
		return err
	}
	parentID, err := linuxPhysicalID(&parentStat)
	if err != nil || parentID != proof.ParentIdentity {
		return errors.New("prepared destination was replaced")
	}
	fd, err := unix.Openat(dir, filepath.Base(proof.File.Path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), proof.File.Path)
	defer f.Close()
	var before, after unix.Stat_t
	if err = unix.Fstat(fd, &before); err != nil {
		return err
	}
	actual, err := linuxPhysicalID(&before)
	if err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || actual != proof.Identity || before.Size < 0 || uint64(before.Size) != proof.File.SizeBytes {
		return errors.New("prepared file identity or size differs")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return err
	}
	if err = unix.Fstat(fd, &after); err != nil {
		return err
	}
	if before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim || hex.EncodeToString(hash.Sum(nil)) != proof.File.SHA256 {
		return errors.New("prepared file changed or hash differs")
	}
	// Linux fds do not prevent rename/unlink by the same user. Recheck both
	// path identities after hashing; recovery never modifies the candidate.
	currentParent, err := hostwin.IdentifyFile(parent)
	if err != nil || contracts.PhysicalIdentity(currentParent) != proof.ParentIdentity {
		return errors.New("prepared destination was replaced")
	}
	currentFile, err := hostwin.IdentifyFile(proof.File.Path)
	if err != nil || contracts.PhysicalIdentity(currentFile) != proof.Identity {
		return errors.New("prepared file was replaced")
	}
	return nil
}
