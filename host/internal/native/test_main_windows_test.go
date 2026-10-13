package native

import (
	"fmt"
	"os"
	"testing"

	"dwnl/host/internal/windows"
)

// Native and runtime packages both exercise the real per-user task mutex.
// Serialize those suites so go test's parallel package workers are not mistaken
// for competing product connections in a different test.
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
