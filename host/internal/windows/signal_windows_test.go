package windows

import (
	"testing"
	"time"
)

func TestSignalReachesItsWaiterOnlyWhileItExists(t *testing.T) {
	scope := "test-signal-" + t.Name() + time.Now().Format("150405.000000000")
	if raised, err := RaiseSignal(scope); err != nil || raised {
		t.Fatalf("raising a signal nobody created: raised=%v err=%v", raised, err)
	}
	signal, err := CreateSignal(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	waited := make(chan bool, 1)
	go func() { waited <- signal.Wait(stop) }()
	if raised, err := RaiseSignal(scope); err != nil || !raised {
		t.Fatalf("raise: raised=%v err=%v", raised, err)
	}
	select {
	case ok := <-waited:
		if !ok {
			t.Fatal("the waiter missed the signal")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter never woke")
	}
	// Auto-reset: one raise wakes one wait, and a stopped wait reports false.
	go func() { waited <- signal.Wait(stop) }()
	close(stop)
	select {
	case ok := <-waited:
		if ok {
			t.Fatal("a stopped wait reported a raise")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stopped wait never returned")
	}
	signal.Close()
	signal.Close()
	if raised, err := RaiseSignal(scope); err != nil || raised {
		t.Fatalf("raising a closed signal: raised=%v err=%v", raised, err)
	}
}
