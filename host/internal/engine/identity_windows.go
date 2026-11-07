//go:build windows

package engine

import (
	"errors"
	"fmt"

	win "golang.org/x/sys/windows"
)

// fileIdentity names one version of a file: its volume and NTFS file index,
// size, and creation and last-write times. Rewriting, replacing or moving
// another file into its place changes the name.
func fileIdentity(path string) (string, error) {
	name, err := win.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := win.CreateFile(name, win.FILE_READ_ATTRIBUTES, win.FILE_SHARE_READ|win.FILE_SHARE_WRITE|win.FILE_SHARE_DELETE, nil, win.OPEN_EXISTING, win.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", err
	}
	defer win.CloseHandle(handle)
	var info win.ByHandleFileInformation
	if err = win.GetFileInformationByHandle(handle, &info); err != nil {
		return "", err
	}
	if info.FileAttributes&(win.FILE_ATTRIBUTE_DIRECTORY|win.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return "", errors.New("not a regular local file")
	}
	return fmt.Sprintf("%08x-%08x%08x-%08x%08x-%d-%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow,
		info.FileSizeHigh, info.FileSizeLow, info.CreationTime.Nanoseconds(), info.LastWriteTime.Nanoseconds()), nil
}
