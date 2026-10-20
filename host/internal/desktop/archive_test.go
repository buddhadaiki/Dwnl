package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostwin "dwnl/host/internal/windows"
)

func archive(t *testing.T, s *Service) int {
	t.Helper()
	result, err := s.Dispatch(context.Background(), "archiveHistory", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	return result.(ArchiveResult).Archived
}
func canceledFixture(t *testing.T) (Config, *Service, enqueuePayload, Task) {
	t.Helper()
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	p := selection(t, s, "archive-request")
	task := enqueue(t, s, p)
	command(t, s, "cancel", `{"id":"`+task.ID+`"}`)
	return c, s, p, task
}

func TestArchiveRestoresCapacityAndKeepsDurableIdempotency(t *testing.T) {
	c, s, p, first := canceledFixture(t)
	s.mu.Lock()
	template := s.tasks[0]
	for len(s.tasks) < maxTasks {
		r := template
		r.Task.ID = fmt.Sprintf("task-%d", len(s.tasks))
		r.Task.RequestID = fmt.Sprintf("receipt-%d", len(s.tasks))
		s.tasks = append(s.tasks, r)
	}
	err := s.persist()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := archive(t, s); got != 200 {
		t.Fatal("archived", got)
	}
	if len(snapshot(t, s).Tasks) != 0 {
		t.Fatal("active snapshot retained archived receipts")
	}
	if got := enqueue(t, s, p); got.ID != first.ID || got.State != "canceled" {
		t.Fatal("receipt changed")
	}
	next := enqueue(t, s, selection(t, s, "new-request"))
	if next.ID == first.ID {
		t.Fatal("new request not accepted")
	}
	s.Close()
	c.Engine = newFake()
	s2 := startService(t, c)
	if got := enqueue(t, s2, p); got.ID != first.ID || got.State != "canceled" {
		t.Fatal("archived replay after restart changed")
	}
	if len(snapshot(t, s2).Tasks) != 1 {
		t.Fatal("archived history loaded into recent task list")
	}
	conflict := p
	conflict.VideoOptionID = "another-option"
	data, _ := json.Marshal(conflict)
	if _, err = s2.Dispatch(context.Background(), "enqueue", data); !errors.Is(err, ErrArchiveConflict) {
		t.Fatalf("archived conflict: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(c.StateRoot, "desktop-history"))
	if err != nil || len(entries) != 200 {
		t.Fatalf("archived receipts: %d %v", len(entries), err)
	}
}

func TestArchiveDuringDownloadDoesNotShiftActiveTask(t *testing.T) {
	f := newFake()
	s := startService(t, fixtureConfig(t, f))
	first := enqueue(t, s, selection(t, s, "first"))
	waitStarted(t, f)
	f.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s, first.ID) == "completed" })
	second := enqueue(t, s, selection(t, s, "second"))
	waitStarted(t, f)
	if got := archive(t, s); got != 1 {
		t.Fatalf("archived %d while second active", got)
	}
	if tasks := snapshot(t, s).Tasks; len(tasks) != 1 || tasks[0].ID != second.ID {
		t.Fatal("wrong active task retained")
	}
	f.gate <- struct{}{}
	waitFor(t, func() bool { return state(t, s, second.ID) == "completed" })
	if snapshot(t, s).Tasks[0].CompletedFile == nil {
		t.Fatal("completion attached to wrong task")
	}
}

func TestArchivePreservesQueuedInterruptedAndLegacy(t *testing.T) {
	_, s, _, _ := canceledFixture(t)
	queued := enqueue(t, s, selection(t, s, "queued"))
	interrupted := enqueue(t, s, selection(t, s, "interrupted"))
	s.mu.Lock()
	s.tasks[2].Task.State = "interrupted"
	err := s.persist()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := archive(t, s); got != 1 {
		t.Fatal(got)
	}
	if state(t, s, queued.ID) != "queued" || state(t, s, interrupted.ID) != "interrupted" {
		t.Fatal("pending task archived")
	}
	if got := archive(t, s); got != 0 {
		t.Fatal("repeated archive should be empty")
	}
}

func TestArchiveRefusesConflictingReceiptWithoutOverwrite(t *testing.T) {
	_, s, _, task := canceledFixture(t)
	s.mu.Lock()
	divergent := s.tasks[0]
	divergent.Task.Title = "different title"
	err := s.writeArchived(divergent)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.archivePath(task.RequestID)
	before, _ := os.ReadFile(path)
	if _, err = s.Dispatch(context.Background(), "archiveHistory", json.RawMessage(`{}`)); !errors.Is(err, ErrArchiveConflict) {
		t.Fatalf("divergent archive: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || len(snapshot(t, s).Tasks) != 1 {
		t.Fatal("conflict changed archive or dropped queue")
	}
}

func TestCorruptArchivePreventsDuplicateEnqueue(t *testing.T) {
	_, s, p, _ := canceledFixture(t)
	archive(t, s)
	path, _ := s.archivePath(p.RequestID)
	original, _ := os.ReadFile(path)
	for _, bad := range [][]byte{[]byte(`{"version":1,"record":`), []byte(strings.Replace(string(original), `"version":1`, `"version":99`, 1))} {
		if err := os.WriteFile(path, bad, 0600); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(p)
		if _, err := s.Dispatch(context.Background(), "enqueue", data); !errors.Is(err, ErrCorruptArchive) {
			t.Fatalf("corrupt archived replay: %v", err)
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(bad) || len(snapshot(t, s).Tasks) != 0 {
			t.Fatal("corrupt receipt overwritten or duplicate accepted")
		}
	}
	if _, err := s.readArchived("../outside"); !errors.Is(err, ErrCorruptArchive) {
		t.Fatal("path traversal accepted")
	}
}

func TestArchiveQueueSaveFailureRollsBackAndRecovers(t *testing.T) {
	c, s, p, task := canceledFixture(t)
	unlock, err := hostwin.LockPath(filepath.Join(c.StateRoot, "desktop-queue.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Dispatch(context.Background(), "archiveHistory", json.RawMessage(`{}`))
	unlock()
	if err == nil {
		t.Skip("platform permits replacing an open queue file; Windows exercises rollback")
	}
	if len(snapshot(t, s).Tasks) != 1 {
		t.Fatal("failed queue save removed in-memory task")
	}
	receipt, readErr := s.readArchived(p.RequestID)
	if readErr != nil || receipt == nil {
		t.Fatal("durable archive receipt missing")
	}
	s.Close()
	c.Engine = newFake()
	s2 := startService(t, c)
	if got := archive(t, s2); got != 1 {
		t.Fatal("partial archive was not repeatable")
	}
	if got := enqueue(t, s2, p); got.ID != task.ID {
		t.Fatal("partial archive lost deduplication")
	}
}

func TestArchiveRequestIDsRemainCaseSensitiveOnWindows(t *testing.T) {
	_, s, _, _ := canceledFixture(t)
	first := selection(t, s, "CaseID")
	one := enqueue(t, s, first)
	command(t, s, "cancel", `{"id":"`+one.ID+`"}`)
	second := selection(t, s, "caseid")
	two := enqueue(t, s, second)
	command(t, s, "cancel", `{"id":"`+two.ID+`"}`)
	archive(t, s)
	if got := enqueue(t, s, first); got.ID != one.ID {
		t.Fatal("case-sensitive receipt mismatch")
	}
	if got := enqueue(t, s, second); got.ID != two.ID {
		t.Fatal("lowercase receipt mismatch")
	}
}
