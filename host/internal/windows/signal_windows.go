package windows

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"unsafe"

	win "golang.org/x/sys/windows"
)

// Signal is a named event private to the current user. One process waits on
// it and another raises it; it carries no data. The installer uses it to ask a
// running Dwnl to quit, which the extension bridge has no way to reach.
type Signal struct {
	handle win.Handle
	once   sync.Once
}

func signalName(scope string) (*uint16, error) {
	id, err := UserID()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(scope))
	return win.UTF16PtrFromString(`Local\Dwnl-` + id + "-signal-" + hex.EncodeToString(sum[:16]))
}

// CreateSignal creates the auto-reset event for scope. Callers hold a lease
// for the same scope, so there is one waiter at a time.
func CreateSignal(scope string) (*Signal, error) {
	name, err := signalName(scope)
	if err != nil {
		return nil, err
	}
	sd, err := privateSecurity()
	if err != nil {
		return nil, err
	}
	sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := win.CreateEvent(&sa, 0, 0, name)
	if h == 0 {
		return nil, err
	}
	// An event that already exists belongs to this user as well (the name
	// carries the user's SID and the object is private), so it is reused.
	return &Signal{handle: h}, nil
}

// Wait blocks until the signal is raised (true), or until stop closes or the
// signal is closed (false).
func (s *Signal) Wait(stop <-chan struct{}) bool {
	h := s.handle
	for {
		status, err := win.WaitForSingleObject(h, 250)
		if err != nil {
			return false
		}
		if status == win.WAIT_OBJECT_0 {
			return true
		}
		select {
		case <-stop:
			return false
		default:
		}
	}
}

func (s *Signal) Close() {
	if s != nil {
		s.once.Do(func() { win.CloseHandle(s.handle) })
	}
}

// RaiseSignal sets the event for scope. It reports false, without error, when
// no process has created it.
func RaiseSignal(scope string) (bool, error) {
	name, err := signalName(scope)
	if err != nil {
		return false, err
	}
	h, err := win.OpenEvent(win.EVENT_MODIFY_STATE, false, name)
	if errors.Is(err, win.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer win.CloseHandle(h)
	return true, win.SetEvent(h)
}
