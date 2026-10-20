package storage

import (
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"errors"
	win "golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func validLocalPath(path string) bool {
	if !privateText(path, 4096) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' || !((volume[0] >= 'A' && volume[0] <= 'Z') || (volume[0] >= 'a' && volume[0] <= 'z')) {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, volume+string(os.PathSeparator)), string(os.PathSeparator)) {
		if part != "" && (strings.ContainsAny(part, `<>:"/\|?*`) || strings.TrimRight(part, " .") != part) {
			return false
		}
	}
	return true
}

func verifyPreparedFile(proof contracts.PreparedFinalization) error {
	parent := filepath.Dir(proof.File.Path)
	unlock, err := hostwin.LockPath(parent, false)
	if err != nil {
		return err
	}
	defer unlock()
	id, err := hostwin.IdentifyFile(parent)
	if err != nil {
		return err
	}
	if contracts.PhysicalIdentity(id) != proof.ParentIdentity {
		return errors.New("prepared destination was replaced")
	}
	if err = hostwin.CheckPath(proof.File.Path, false); err != nil {
		return err
	}
	p, err := win.UTF16PtrFromString(proof.File.Path)
	if err != nil {
		return err
	}
	// This handle denies both writes and deletion during all checks. Identity,
	// size and hash are read from this one object, never a reopened pathname.
	h, err := win.CreateFile(p, win.GENERIC_READ|win.FILE_READ_ATTRIBUTES, win.FILE_SHARE_READ, nil, win.OPEN_EXISTING, win.FILE_FLAG_OPEN_REPARSE_POINT|win.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), proof.File.Path)
	if f == nil {
		win.CloseHandle(h)
		return errors.New("invalid prepared file handle")
	}
	defer f.Close()
	var info win.ByHandleFileInformation
	if err = win.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	actual := contracts.PhysicalIdentity{VolumeSerialNumber: info.VolumeSerialNumber, FileIndexHigh: info.FileIndexHigh, FileIndexLow: info.FileIndexLow}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if info.FileAttributes&(win.FILE_ATTRIBUTE_DIRECTORY|win.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || actual != proof.Identity || size != proof.File.SizeBytes {
		return errors.New("prepared file identity or size differs")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != proof.File.SHA256 {
		return errors.New("prepared file hash differs")
	}
	return nil
}
