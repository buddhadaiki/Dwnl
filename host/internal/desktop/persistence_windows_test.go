package desktop

import (
	hostwin "dwnl/host/internal/windows"
	"path/filepath"
	"testing"
	"time"
)

func TestEnqueueSurvivesBriefWindowsSharingDenial(t *testing.T) {
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	p := selection(t, s, "transient-lock")
	unlock, err := hostwin.LockPath(filepath.Join(c.StateRoot, "desktop-queue.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() { time.Sleep(40 * time.Millisecond); unlock(); close(released) }()
	task := enqueue(t, s, p)
	<-released
	if task.ID == "" {
		t.Fatal("no durable acceptance")
	}
}
