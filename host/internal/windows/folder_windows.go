package windows

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	win "golang.org/x/sys/windows"
)

const (
	windowNCDestroy       = 0x0082
	folderCanceledHRESULT = 0x800704c7
)

var (
	folderUser32         = win.NewLazySystemDLL("user32.dll")
	folderComctl32       = win.NewLazySystemDLL("comctl32.dll")
	folderPostMessage    = folderUser32.NewProc("PostMessageW")
	folderSetSubclass    = folderComctl32.NewProc("SetWindowSubclass")
	folderRemoveSubclass = folderComctl32.NewProc("RemoveWindowSubclass")
	folderDefSubclass    = folderComctl32.NewProc("DefSubclassProc")
	folderSubclassProc   uintptr
	folderToken          atomic.Uintptr
	folderDialogs        = struct {
		sync.Mutex
		entries map[uintptr]*folderDialog
	}{entries: make(map[uintptr]*folderDialog)}
	// CLSID_FileOpenDialog and IID_IFileOpenDialog from shobjidl_core.h.
	folderClassID     = win.GUID{Data1: 0xdc1c5a9c, Data2: 0xe88a, Data3: 0x4dde, Data4: [8]byte{0xa5, 0xa1, 0x60, 0xf8, 0x2a, 0x20, 0xae, 0xf7}}
	folderInterfaceID = win.GUID{Data1: 0xd57c7288, Data2: 0xd4ad, Data3: 0x4768, Data4: [8]byte{0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60}}
)

func init() { folderSubclassProc = syscall.NewCallback(folderSubclassCallback) }

// IFileOpenDialog inherits IUnknown, IModalWindow and IFileDialog, in this
// order. Slots used below are the published COM ABI: Release=2, Show=3,
// SetOptions=9, GetOptions=10, SetTitle=17, GetResult=20 and Close=23.
type folderFileDialog struct{ vtable *[29]uintptr }
type folderShellItem struct{ vtable *[8]uintptr }

// Keep Go storage passed as uintptr live and immovable through the COM call.
//
//go:uintptrescapes
func (d *folderFileDialog) invoke(slot int, args ...uintptr) uintptr {
	params := append([]uintptr{uintptr(unsafe.Pointer(d))}, args...)
	result, _, _ := syscall.SyscallN(d.vtable[slot], params...)
	goruntime.KeepAlive(d)
	return result
}

func folderHRESULT(operation string, result uintptr) error {
	if int32(result) < 0 {
		return fmt.Errorf("folder dialog %s failed (HRESULT 0x%08x)", operation, uint32(result))
	}
	return nil
}

type folderDialog struct {
	ctx     context.Context
	token   uintptr
	message uintptr
	hwnd    atomic.Uintptr
	showing atomic.Bool
	// COM pointers are accessed only on the owning locked OS thread.
	dialog *folderFileDialog
}

func folderDialogByToken(token uintptr) *folderDialog {
	folderDialogs.Lock()
	defer folderDialogs.Unlock()
	return folderDialogs.entries[token]
}

func (d *folderDialog) requestClose() {
	if hwnd := d.hwnd.Load(); hwnd != 0 {
		// Only a token-bearing private message crosses threads. A stale queued
		// message cannot close a new window that reuses this HWND.
		folderPostMessage.Call(hwnd, d.message, d.token, 0)
	}
}

func folderSubclassCallback(hwnd, message, wparam, lparam, subclassID, token uintptr) uintptr {
	d := folderDialogByToken(token)
	if d != nil {
		if message == d.message && wparam == token {
			if d.ctx.Err() != nil {
				// The modal Show loop dispatches this on the same COM apartment
				// that owns the dialog. No COM pointer crosses thread boundaries.
				d.dialog.invoke(23, folderCanceledHRESULT)
			}
			return 0
		}
		if message == windowNCDestroy {
			d.hwnd.CompareAndSwap(hwnd, 0)
			folderRemoveSubclass.Call(hwnd, folderSubclassProc, subclassID)
		}
	}
	result, _, _ := folderDefSubclass.Call(hwnd, message, wparam, lparam)
	return result
}

// ChooseFolder uses the modern Windows folder dialog. A message-only window on
// its COM apartment translates context cancellation into IFileDialog.Close.
// This does not depend on SHBrowseForFolder callbacks, which modern shell
// replacements can omit even when they display a working picker.
func ChooseFolder(ctx context.Context) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	ole := win.NewLazySystemDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if err := folderHRESULT("initialization", hr); err != nil {
		return "", false, err
	}
	defer ole.NewProc("CoUninitialize").Call()

	var dialog *folderFileDialog
	hr, _, _ = ole.NewProc("CoCreateInstance").Call(
		uintptr(unsafe.Pointer(&folderClassID)), 0, 1,
		uintptr(unsafe.Pointer(&folderInterfaceID)), uintptr(unsafe.Pointer(&dialog)))
	if err := folderHRESULT("creation", hr); err != nil {
		return "", false, err
	}
	if dialog == nil {
		return "", false, errors.New("folder dialog unavailable")
	}
	defer dialog.invoke(2)

	var options uint32
	if err := folderHRESULT("options", dialog.invoke(10, uintptr(unsafe.Pointer(&options)))); err != nil {
		return "", false, err
	}
	if err := folderHRESULT("options", dialog.invoke(9, uintptr(options|0x20|0x40))); err != nil {
		return "", false, err
	}
	title, _ := win.UTF16PtrFromString("Escolha a pasta de downloads do Dwnl")
	if err := folderHRESULT("title", dialog.invoke(17, uintptr(unsafe.Pointer(title)))); err != nil {
		return "", false, err
	}

	messageName, _ := win.UTF16PtrFromString("Dwnl.FolderPicker.Cancel")
	cancelMessage, _, _ := folderUser32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(messageName)))
	if cancelMessage == 0 {
		return "", false, errors.New("folder dialog cancellation message unavailable")
	}

	token := folderToken.Add(1)
	d := &folderDialog{ctx: ctx, token: token, message: cancelMessage, dialog: dialog}
	folderDialogs.Lock()
	folderDialogs.entries[token] = d
	folderDialogs.Unlock()
	defer func() {
		folderDialogs.Lock()
		delete(folderDialogs.entries, token)
		folderDialogs.Unlock()
	}()

	// HWND_MESSAGE (-3) creates no visible window or taskbar item. Its lifetime
	// is entirely ours, including on systems replacing legacy shell dialogs.
	className, _ := win.UTF16PtrFromString("STATIC")
	hwnd, _, _ := folderUser32.NewProc("CreateWindowExW").Call(
		0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
	if hwnd == 0 {
		return "", false, errors.New("folder dialog cancellation window unavailable")
	}
	defer func() {
		d.hwnd.Store(0)
		folderUser32.NewProc("DestroyWindow").Call(hwnd)
	}()
	ok, _, _ := folderSetSubclass.Call(hwnd, folderSubclassProc, token, token)
	if ok == 0 {
		return "", false, errors.New("folder dialog cancellation unavailable")
	}
	d.hwnd.Store(hwnd)
	stopCancel := context.AfterFunc(ctx, d.requestClose)
	defer stopCancel()

	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	d.showing.Store(true)
	hr = dialog.invoke(3, 0)
	d.showing.Store(false)
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if uint32(hr) == folderCanceledHRESULT {
		return "", true, nil
	}
	if err := folderHRESULT("display", hr); err != nil {
		return "", false, err
	}

	var item *folderShellItem
	if err := folderHRESULT("selection", dialog.invoke(20, uintptr(unsafe.Pointer(&item)))); err != nil {
		return "", false, err
	}
	if item == nil {
		return "", false, errors.New("selected destination is unavailable")
	}
	defer syscall.SyscallN(item.vtable[2], uintptr(unsafe.Pointer(item)))
	var path *uint16
	hr, _, _ = syscall.SyscallN(item.vtable[5], uintptr(unsafe.Pointer(item)), 0x80058000, uintptr(unsafe.Pointer(&path))) // SIGDN_FILESYSPATH
	if err := folderHRESULT("path", hr); err != nil {
		return "", false, err
	}
	if path == nil {
		return "", false, errors.New("selected destination has no local path")
	}
	defer ole.NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	return win.UTF16PtrToString(path), false, nil
}
