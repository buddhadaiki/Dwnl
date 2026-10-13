package runtime

import (
	"fmt"
	"os"
	"testing"

	"dwnl/host/internal/windows"
)

func TestMain(m *testing.M) {
	lease, _, err := windows.AcquireLease("dwnl-lifecycle-test-suite", true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "test suite mutex:", err)
		os.Exit(1)
	}
	code := m.Run()
	lease.Close()
	os.Exit(code)
}
