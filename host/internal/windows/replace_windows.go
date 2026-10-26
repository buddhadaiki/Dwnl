package windows

import (
	"errors"
	win "golang.org/x/sys/windows"
	"time"
)

// ReplaceFile atomically replaces target with source. A scanner can briefly
// deny replacement even in the owned private directory. Retry only Windows
// sharing/access denials for at most 200ms; persistent denial is returned to
// the caller before it can acknowledge the write.
func ReplaceFile(source, target string) error {
	from, err := win.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := win.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		err = win.MoveFileEx(from, to, win.MOVEFILE_REPLACE_EXISTING|win.MOVEFILE_WRITE_THROUGH)
		if err == nil {
			return nil
		}
		if (!errors.Is(err, win.ERROR_SHARING_VIOLATION) && !errors.Is(err, win.ERROR_ACCESS_DENIED)) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
