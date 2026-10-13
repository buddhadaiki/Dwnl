package windows

import (
	"context"
	"errors"
	win "golang.org/x/sys/windows"
	goruntime "runtime"
	"unsafe"
)

// ChooseFolder shows the OS folder picker only after the session accepts a
// chooseDestination user gesture. It does not trust a browser-supplied path.
func ChooseFolder(ctx context.Context) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	ole := win.NewLazySystemDLL("ole32.dll")
	shell := win.NewLazySystemDLL("shell32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		return "", false, errors.New("folder dialog unavailable")
	}
	defer ole.NewProc("CoUninitialize").Call()
	type browseInfo struct {
		Owner       win.Handle
		Root        uintptr
		DisplayName *uint16
		Title       *uint16
		Flags       uint32
		Callback    uintptr
		Param       uintptr
		Image       int32
	}
	title, _ := win.UTF16PtrFromString("Choose the Dwnl download folder")
	var display [260]uint16
	info := browseInfo{DisplayName: &display[0], Title: title, Flags: 0x01 | 0x40 | 0x10}
	pidl, _, _ := shell.NewProc("SHBrowseForFolderW").Call(uintptr(unsafe.Pointer(&info)))
	if pidl == 0 {
		return "", true, nil
	}
	defer ole.NewProc("CoTaskMemFree").Call(pidl)
	var path [32768]uint16
	ok, _, _ := shell.NewProc("SHGetPathFromIDListEx").Call(pidl, uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)), 0)
	if ok == 0 {
		return "", false, errors.New("selected destination has no local path")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	return win.UTF16ToString(path[:]), false, nil
}
