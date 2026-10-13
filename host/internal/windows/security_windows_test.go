package windows

import (
	"testing"
)

func TestMutexBusyAndReleasedAcrossConnections(t *testing.T) {
	first, busy, err := AcquireLease("test-connection-lock", false)
	if err != nil || busy {
		t.Fatalf("first %v %v", busy, err)
	}
	defer first.Close()
	second, busy, err := AcquireLease("test-connection-lock", false)
	if second != nil {
		second.Close()
	}
	if err != nil || !busy {
		t.Fatalf("second %v %v", busy, err)
	}
	first.Close()
	last, busy, err := AcquireLease("test-connection-lock", false)
	if err != nil || busy {
		t.Fatalf("after release %v %v", busy, err)
	}
	last.Close()
}
