// Package windows owns Windows-only process, filesystem and user identity boundaries.
package windows

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	win "golang.org/x/sys/windows"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"unsafe"
)

func UserID() (string, error) {
	u, err := win.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}
func privateSecurity() (*win.SECURITY_DESCRIPTOR, error) {
	id, err := UserID()
	if err != nil {
		return nil, err
	}
	return win.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + id + ")")
}
func ProtectDirectory(path string) error {
	if err := CheckPath(path, false); err != nil {
		return err
	}
	sd, err := privateSecurity()
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return win.SetNamedSecurityInfo(path, win.SE_FILE_OBJECT, win.DACL_SECURITY_INFORMATION|win.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// CheckPath rejects every existing reparse component, including junctions. It
// permits missing tail components only when explicitly requested for creation.
func CheckPath(path string, allowMissing bool) error {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return errors.New("path must be absolute")
	}
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if len(volume) != 2 || volume[1] != ':' {
		return errors.New("only local drive paths are permitted")
	}
	cur := volume + string(os.PathSeparator)
	parts := strings.Split(strings.TrimPrefix(clean, cur), string(os.PathSeparator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		p, err := win.UTF16PtrFromString(cur)
		if err != nil {
			return err
		}
		attr, err := win.GetFileAttributes(p)
		if err != nil {
			if allowMissing && (errors.Is(err, win.ERROR_FILE_NOT_FOUND) || errors.Is(err, win.ERROR_PATH_NOT_FOUND)) {
				return nil
			}
			return err
		}
		if attr&win.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("reparse points are not permitted")
		}
	}
	return nil
}
func IsWithin(root, path string) bool {
	r, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator)) && !filepath.IsAbs(r)
}
func DefaultPaths() (stateRoot, destination string, err error) {
	local, err := win.KnownFolderPath(win.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", "", err
	}
	downloads, err := win.KnownFolderPath(win.FOLDERID_Downloads, 0)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(local, "Dwnl", "state"), filepath.Join(downloads, "Dwnl"), nil
}

// Lease owns a named mutex on one OS thread, since Windows mutex ownership is
// thread-affine. The kernel marks it abandoned if the owning host exits.
type Lease struct {
	release chan struct{}
	done    chan struct{}
	once    sync.Once
}

func AcquireLease(scope string, wait bool) (*Lease, bool, error) {
	id, err := UserID()
	if err != nil {
		return nil, false, err
	}
	sum := sha256.Sum256([]byte(scope))
	name := "Global\\Dwnl-" + id + "-" + hex.EncodeToString(sum[:16])
	result := make(chan struct {
		l    *Lease
		busy bool
		err  error
	}, 1)
	go func() {
		goruntime.LockOSThread()
		defer goruntime.UnlockOSThread()
		sd, err := privateSecurity()
		if err != nil {
			result <- struct {
				l    *Lease
				busy bool
				err  error
			}{err: err}
			return
		}
		sa := win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), SecurityDescriptor: sd}
		n, _ := win.UTF16PtrFromString(name)
		h, err := win.CreateMutex(&sa, false, n)
		if err != nil && !errors.Is(err, win.ERROR_ALREADY_EXISTS) {
			result <- struct {
				l    *Lease
				busy bool
				err  error
			}{err: err}
			return
		}
		defer win.CloseHandle(h)
		timeout := uint32(0)
		if wait {
			timeout = win.INFINITE
		}
		status, err := win.WaitForSingleObject(h, timeout)
		if err != nil {
			result <- struct {
				l    *Lease
				busy bool
				err  error
			}{err: err}
			return
		}
		if status == uint32(win.WAIT_TIMEOUT) {
			result <- struct {
				l    *Lease
				busy bool
				err  error
			}{busy: true}
			return
		}
		if status != win.WAIT_OBJECT_0 && status != win.WAIT_ABANDONED {
			result <- struct {
				l    *Lease
				busy bool
				err  error
			}{err: fmt.Errorf("mutex wait failed: %d", status)}
			return
		}
		l := &Lease{release: make(chan struct{}), done: make(chan struct{})}
		result <- struct {
			l    *Lease
			busy bool
			err  error
		}{l: l}
		<-l.release
		win.ReleaseMutex(h)
		close(l.done)
	}()
	r := <-result
	return r.l, r.busy, r.err
}
func (l *Lease) Close() {
	if l != nil {
		l.once.Do(func() { close(l.release); <-l.done })
	}
}
