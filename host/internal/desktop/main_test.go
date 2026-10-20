package desktop

import (
	"dwnl/host/internal/windows"
	"fmt"
	"os"
	"testing"
)

// The legacy runtime suite uses the same real per-user execution mutex.
func TestMain(m *testing.M) {
	lease, _, err := windows.AcquireLease("dwnl-lifecycle-test-suite", true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	lease.Close()
	os.Exit(code)
}
