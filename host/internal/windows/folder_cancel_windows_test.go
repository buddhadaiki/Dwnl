package windows

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestChooseFolderAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path, selected, err := ChooseFolder(ctx)
	if path != "" || selected || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context must not open or select: path=%q selected=%v error=%v", path, selected, err)
	}
}

func TestChooseFolderNativeCancellation(t *testing.T) {
	if os.Getenv("DWNL_TEST_NATIVE_PICKER") != "1" {
		t.Skip("set DWNL_TEST_NATIVE_PICKER=1 in an interactive Windows session")
	}
	type outcome struct {
		path     string
		canceled bool
		err      error
	}
	var previousToken uintptr
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		result := make(chan outcome, 1)
		go func() {
			path, canceled, err := ChooseFolder(ctx)
			result <- outcome{path, canceled, err}
		}()
		var dialog *folderDialog
		deadline := time.NewTimer(15 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
	waitForWindow:
		for {
			select {
			case value := <-result:
				deadline.Stop()
				ticker.Stop()
				t.Fatalf("dialog returned before initialized: %+v", value)
			case <-deadline.C:
				ticker.Stop()
				folderDialogs.Lock()
				count := len(folderDialogs.entries)
				folderDialogs.Unlock()
				t.Fatalf("native picker did not initialize in time; registered callbacks: %d", count)
			case <-ticker.C:
				folderDialogs.Lock()
				for token, candidate := range folderDialogs.entries {
					if token != previousToken && candidate.hwnd.Load() != 0 && candidate.showing.Load() {
						dialog = candidate
						break
					}
				}
				folderDialogs.Unlock()
				if dialog != nil {
					break waitForWindow
				}
			}
		}
		deadline.Stop()
		ticker.Stop()
		if previousToken != 0 {
			// Simulate delivery of a stale cancellation to a later dialog. Even
			// if Windows reuses its HWND, the token must not close that dialog.
			folderPostMessage.Call(dialog.hwnd.Load(), dialog.message, previousToken, 0)
			select {
			case value := <-result:
				t.Fatalf("stale token closed the new picker: %+v", value)
			case <-time.After(100 * time.Millisecond):
			}
		}
		cancel()
		select {
		case value := <-result:
			if value.path != "" || value.canceled || !errors.Is(value.err, context.Canceled) {
				t.Fatalf("context cancellation result: %+v", value)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("context cancellation did not close native picker")
		}
		if dialog.hwnd.Load() != 0 || folderDialogByToken(dialog.token) != nil {
			t.Fatal("closed dialog retained native handle or callback state")
		}
		previousToken = dialog.token
	}
}
