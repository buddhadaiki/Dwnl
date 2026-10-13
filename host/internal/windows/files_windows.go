package windows

import (
	"errors"
	win "golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

func openLocked(path string, access uint32) (win.Handle, error) {
	p, err := win.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, err := win.CreateFile(p, access, win.FILE_SHARE_READ|win.FILE_SHARE_WRITE, nil, win.OPEN_EXISTING, win.FILE_FLAG_OPEN_REPARSE_POINT|win.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, err
	}
	var info win.ByHandleFileInformation
	if err = win.GetFileInformationByHandle(h, &info); err != nil {
		win.CloseHandle(h)
		return 0, err
	}
	if info.FileAttributes&win.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		win.CloseHandle(h)
		return 0, errors.New("reparse points are not permitted")
	}
	return h, nil
}

// LockPath pins existing path components against rename/deletion while callers
// inspect or mutate descendants. It never follows reparse points.
func LockPath(path string, allowMissing bool) (func(), error) {
	if err := CheckPath(path, allowMissing); err != nil {
		return nil, err
	}
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	cur := volume + string(os.PathSeparator)
	var held []win.Handle
	closeAll := func() {
		for i := len(held) - 1; i >= 0; i-- {
			win.CloseHandle(held[i])
		}
	}
	h, err := openLocked(cur, win.FILE_READ_ATTRIBUTES)
	if err != nil {
		return nil, err
	}
	held = append(held, h)
	for _, part := range strings.Split(strings.TrimPrefix(clean, cur), string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		h, err = openLocked(cur, win.FILE_READ_ATTRIBUTES)
		if err != nil {
			if allowMissing && (errors.Is(err, win.ERROR_FILE_NOT_FOUND) || errors.Is(err, win.ERROR_PATH_NOT_FOUND)) {
				return closeAll, nil
			}
			closeAll()
			return nil, err
		}
		held = append(held, h)
	}
	return closeAll, nil
}
func CreateDirectories(path string, private bool) error {
	unlock, err := LockPath(path, true)
	if err != nil {
		return err
	}
	defer unlock()
	if err = os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if err = CheckPath(path, false); err != nil {
		return err
	}
	if private {
		return ProtectDirectory(path)
	}
	return nil
}

// RemoveOwnedTree deletes using handles that pin each object, never by following
// a replacement link. The caller must first verify its task ownership marker.
func RemoveOwnedTree(path string, validateOwnership func() error) error {
	unlock, err := LockPath(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unlock()
	return removeLocked(path, validateOwnership)
}
func removeLocked(path string, validateOwnership func() error) error {
	h, err := openLocked(path, win.DELETE|win.GENERIC_READ)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(h), path)
	if file == nil {
		win.CloseHandle(h)
		return errors.New("invalid owned handle")
	}
	defer file.Close()
	var info win.ByHandleFileInformation
	if err = win.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if validateOwnership != nil {
		if err = validateOwnership(); err != nil {
			return err
		}
	}
	if info.FileAttributes&win.FILE_ATTRIBUTE_DIRECTORY != 0 {
		entries, err := file.Readdirnames(-1)
		if err != nil {
			return err
		}
		// Validate every child before deleting any child of this directory.
		for _, entry := range entries {
			if err = CheckPath(filepath.Join(path, entry), false); err != nil {
				return err
			}
		}
		for _, entry := range entries {
			if err = removeLocked(filepath.Join(path, entry), nil); err != nil {
				return err
			}
		}
	}
	disposition := byte(1)
	return win.SetFileInformationByHandle(h, win.FileDispositionInfo, (*byte)(unsafe.Pointer(&disposition)), 1)
}

type FileIdentity struct {
	VolumeSerialNumber uint32
	FileIndexHigh      uint32
	FileIndexLow       uint32
}

func identity(h win.Handle) (FileIdentity, error) {
	var info win.ByHandleFileInformation
	err := win.GetFileInformationByHandle(h, &info)
	return FileIdentity{info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow}, err
}
func IdentifyFile(path string) (FileIdentity, error) {
	h, err := openLocked(path, win.FILE_READ_ATTRIBUTES)
	if err != nil {
		return FileIdentity{}, err
	}
	defer win.CloseHandle(h)
	return identity(h)
}

// RemoveExactFile requires the identity captured when an owned intermediate was
// created. A replaced pathname or reparse point is preserved and rejected.
func RemoveExactFile(path string, want FileIdentity) error {
	unlock, err := LockPath(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unlock()
	h, err := openLocked(path, win.DELETE|win.FILE_READ_ATTRIBUTES)
	if err != nil {
		return err
	}
	defer win.CloseHandle(h)
	got, err := identity(h)
	if err != nil {
		return err
	}
	if got != want {
		return errors.New("owned intermediate was replaced")
	}
	var info win.ByHandleFileInformation
	if err = win.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&win.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("intermediate is not a file")
	}
	disposition := byte(1)
	return win.SetFileInformationByHandle(h, win.FileDispositionInfo, &disposition, 1)
}

// fileRenameInfo matches FILE_RENAME_INFO. Flags is the documented four-byte
// union; all bits stay zero for FileRenameInfo (ReplaceIfExists = FALSE).
// RootDirectory is NULL for the documented absolute UTF-16 target form.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  win.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// PublishExactFile renames the file object identified by the open handle,
// never a source pathname looked up again after validation. Both identities
// come from creation in the approved destination. A concurrent source swap is
// either rejected by identity or blocked by the handle's denied delete sharing.
func PublishExactFile(source, target string, want, approvedParent FileIdentity) error {
	if !filepath.IsAbs(source) || !filepath.IsAbs(target) {
		return errors.New("publication paths must be absolute")
	}
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	parent := filepath.Dir(source)
	if !strings.EqualFold(parent, filepath.Dir(target)) || strings.EqualFold(source, target) {
		return errors.New("publication requires a new name in the approved parent")
	}
	name := filepath.Base(target)
	if name == "." || name == ".." || strings.ContainsAny(name, `<>:"/\|?*`) || strings.TrimRight(name, " .") != name {
		return errors.New("invalid publication filename")
	}
	for _, r := range name {
		if r < 32 {
			return errors.New("invalid publication filename")
		}
	}
	unlock, err := LockPath(parent, false)
	if err != nil {
		return err
	}
	defer unlock()
	parentHandle, err := openLocked(parent, win.FILE_READ_ATTRIBUTES|win.FILE_TRAVERSE)
	if err != nil {
		return err
	}
	defer win.CloseHandle(parentHandle)
	actualParent, err := identity(parentHandle)
	if err != nil {
		return err
	}
	if actualParent != approvedParent {
		return errors.New("approved destination was replaced")
	}
	// Also pin and compare the requested target parent. Case-sensitive Windows
	// directories can have different objects whose spellings compare equal.
	unlockTarget, err := LockPath(filepath.Dir(target), false)
	if err != nil {
		return err
	}
	defer unlockTarget()
	targetParent, err := IdentifyFile(filepath.Dir(target))
	if err != nil {
		return err
	}
	if targetParent != approvedParent {
		return errors.New("publication target is outside the approved parent")
	}
	if err = CheckPath(target, true); err != nil {
		return err
	}
	fileHandle, err := openLocked(source, win.DELETE|win.FILE_READ_ATTRIBUTES)
	if err != nil {
		return err
	}
	defer win.CloseHandle(fileHandle)
	actual, err := identity(fileHandle)
	if err != nil {
		return err
	}
	if actual != want {
		return errors.New("owned intermediate was replaced")
	}
	var attributes win.ByHandleFileInformation
	if err = win.GetFileInformationByHandle(fileHandle, &attributes); err != nil {
		return err
	}
	if attributes.FileAttributes&win.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("intermediate is not a file")
	}
	utf16Name, err := win.UTF16FromString(target)
	if err != nil {
		return err
	}
	var layout fileRenameInfo
	offset := int(unsafe.Offsetof(layout.FileName))
	buffer := make([]byte, offset+len(utf16Name)*2)
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.FileNameLength = uint32((len(utf16Name) - 1) * 2)
	copy(unsafe.Slice(&info.FileName[0], len(utf16Name)), utf16Name)
	if err = win.SetFileInformationByHandle(fileHandle, win.FileRenameInfo, &buffer[0], uint32(len(buffer))); err != nil {
		if errors.Is(err, win.ERROR_ALREADY_EXISTS) || errors.Is(err, win.ERROR_FILE_EXISTS) {
			return &os.PathError{Op: "publish", Path: target, Err: os.ErrExist}
		}
		return &os.PathError{Op: "publish", Path: target, Err: err}
	}
	return nil
}
