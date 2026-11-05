package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completedFixture(t *testing.T, requests ...string) (Config, *Service, []enqueuePayload, []Task) {
	t.Helper()
	f := newFake()
	c := fixtureConfig(t, f)
	s := startService(t, c)
	var payloads []enqueuePayload
	var tasks []Task
	for _, request := range requests {
		p := selection(t, s, request)
		task := enqueue(t, s, p)
		f.gate <- struct{}{}
		waitFor(t, func() bool { return state(t, s, task.ID) == "completed" })
		payloads = append(payloads, p)
		tasks = append(tasks, task)
	}
	return c, s, payloads, tasks
}

func taskByID(t *testing.T, s *Service, id string) Task {
	t.Helper()
	for _, task := range snapshot(t, s).Tasks {
		if task.ID == id {
			return task
		}
	}
	t.Fatal("task missing")
	return Task{}
}

func rename(s *Service, id, title string) (Task, error) {
	data, _ := json.Marshal(map[string]string{"id": id, "title": title})
	result, err := s.Dispatch(context.Background(), "renameTask", data)
	if err != nil {
		return Task{}, err
	}
	return result.(Task), nil
}

func TestArchiveOneRecordKeepsTheRestAndItsFile(t *testing.T) {
	_, s, payloads, tasks := completedFixture(t, "first", "second")
	file := taskByID(t, s, tasks[0].ID).CompletedFile.Path
	result, err := s.Dispatch(context.Background(), "archiveHistory", json.RawMessage(`{"id":"`+tasks[0].ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.(ArchiveResult).Archived != 1 {
		t.Fatal("archived", result)
	}
	if remaining := snapshot(t, s).Tasks; len(remaining) != 1 || remaining[0].ID != tasks[1].ID {
		t.Fatal("wrong records left", remaining)
	}
	if _, err = os.Stat(file); err != nil {
		t.Fatal("removing a record touched its file", err)
	}
	if replay := enqueue(t, s, payloads[0]); replay.ID != tasks[0].ID || replay.State != "completed" {
		t.Fatal("removed record lost its receipt", replay)
	}
}

func TestArchiveOneRefusesUnfinishedAndUnknownRecords(t *testing.T) {
	_, s, _, _ := completedFixture(t)
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	queued := enqueue(t, s, selection(t, s, "queued"))
	for _, id := range []string{queued.ID, "missing"} {
		if _, err := s.Dispatch(context.Background(), "archiveHistory", json.RawMessage(`{"id":"`+id+`"}`)); err == nil {
			t.Fatal("archived", id)
		}
	}
	if state(t, s, queued.ID) != "queued" {
		t.Fatal("queued task changed")
	}
}

func TestRenameMovesTheFileAndKeepsTheNameAfterRestart(t *testing.T) {
	c, s, payloads, tasks := completedFixture(t, "rename")
	before := taskByID(t, s, tasks[0].ID).CompletedFile.Path
	renamed, err := rename(s, tasks[0].ID, "  Meu vídeo: final  ")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(before), "Meu vídeo_ final.mp4")
	if renamed.Title != "Meu vídeo: final" || renamed.CompletedFile.Path != want {
		t.Fatalf("renamed to %q at %q", renamed.Title, renamed.CompletedFile.Path)
	}
	if _, err = os.Stat(before); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old file name still present", err)
	}
	if data, err := os.ReadFile(want); err != nil || string(data) != "real verified fixture bytes" {
		t.Fatal("renamed file missing", err)
	}
	s.Close()
	c.Engine = newFake()
	s2 := startService(t, c)
	if got := taskByID(t, s2, tasks[0].ID); got.Title != "Meu vídeo: final" || got.CompletedFile.Path != want || got.State != "completed" {
		t.Fatal("rename not saved", got)
	}
	if replay := enqueue(t, s2, payloads[0]); replay.Title != "Meu vídeo: final" {
		t.Fatal("replay lost the new name", replay)
	}
}

func TestRenameRefusesATakenNameWithoutChanges(t *testing.T) {
	_, s, _, tasks := completedFixture(t, "taken")
	before := taskByID(t, s, tasks[0].ID)
	other := filepath.Join(filepath.Dir(before.CompletedFile.Path), "Outro.mp4")
	if err := os.WriteFile(other, []byte("someone else"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := rename(s, tasks[0].ID, "Outro"); !errors.Is(err, ErrNameTaken) {
		t.Fatal("taken name accepted", err)
	}
	if data, _ := os.ReadFile(other); string(data) != "someone else" {
		t.Fatal("existing file overwritten")
	}
	if after := taskByID(t, s, tasks[0].ID); after.Title != before.Title || after.CompletedFile.Path != before.CompletedFile.Path {
		t.Fatal("record changed", after)
	}
	if _, err := os.Stat(before.CompletedFile.Path); err != nil {
		t.Fatal("original file moved", err)
	}
}

func TestRenameRefusesBadNamesAndUnfinishedDownloads(t *testing.T) {
	_, s, _, tasks := completedFixture(t, "bad")
	for _, title := range []string{"", "   ", "line\nbreak", strings.Repeat("a", 121)} {
		if _, err := rename(s, tasks[0].ID, title); err == nil {
			t.Fatalf("accepted %q", title)
		}
	}
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
	queued := enqueue(t, s, selection(t, s, "queued"))
	if _, err := rename(s, queued.ID, "Novo"); err == nil {
		t.Fatal("renamed an unfinished download")
	}
	if _, err := rename(s, "missing", "Novo"); err == nil {
		t.Fatal("renamed an unknown download")
	}
}

func TestRenamedRecordCanBeRemovedAndRestarted(t *testing.T) {
	c, s, payloads, tasks := completedFixture(t, "renamed-then-removed")
	if _, err := rename(s, tasks[0].ID, "Novo nome"); err != nil {
		t.Fatal(err)
	}
	command(t, s, "archiveHistory", `{"id":"`+tasks[0].ID+`"}`)
	s.Close()
	c.Engine = newFake()
	s2 := startService(t, c)
	if len(snapshot(t, s2).Tasks) != 0 {
		t.Fatal("removed record came back")
	}
	if replay := enqueue(t, s2, payloads[0]); replay.ID != tasks[0].ID || replay.Title != "Novo nome" {
		t.Fatal("receipt does not match the renamed record", replay)
	}
}

func TestRenameRefusesAFileWhoseBytesChangedButNotItsSize(t *testing.T) {
	_, s, _, tasks := completedFixture(t, "edited")
	before := taskByID(t, s, tasks[0].ID)
	edited := strings.Repeat("x", int(before.CompletedFile.SizeBytes))
	if err := os.WriteFile(before.CompletedFile.Path, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := rename(s, tasks[0].ID, "Outro nome"); !errors.Is(err, errFileUnavailable) {
		t.Fatal("renamed content that is not the download", err)
	}
	if data, err := os.ReadFile(before.CompletedFile.Path); err != nil || string(data) != edited {
		t.Fatal("edited file moved", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(before.CompletedFile.Path), "Outro nome.mp4")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file appeared at the new name", err)
	}
	if after := taskByID(t, s, tasks[0].ID); after.Title != before.Title || after.CompletedFile.Path != before.CompletedFile.Path {
		t.Fatal("record changed", after)
	}
}

// stopMidRename saves the rename the way renameTask does before moving the
// file, optionally moves it, and stops the service without finishing.
func stopMidRename(t *testing.T, s *Service, id, title string, moveFile bool) (oldPath, newPath string) {
	t.Helper()
	s.mu.Lock()
	before := s.tasks[s.findTask(id)]
	s.mu.Unlock()
	oldPath = before.Task.CompletedFile.Path
	newPath = filepath.Join(filepath.Dir(oldPath), title+".mp4")
	pending := before
	pending.Task.Title = title
	file := *before.Task.CompletedFile
	file.Path = newPath
	pending.Task.CompletedFile = &file
	pending.RenamedFrom = &renameOrigin{Title: before.Task.Title, Path: oldPath}
	if err := s.replaceRecord(id, before, pending); err != nil {
		t.Fatal(err)
	}
	if moveFile {
		if err := os.Rename(oldPath, newPath); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	return oldPath, newPath
}

func savedQueue(t *testing.T, s *Service) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.store.Root(), "desktop-queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRenameCutShortAfterTheMoveKeepsTheNewName(t *testing.T) {
	c, s, _, tasks := completedFixture(t, "moved")
	_, newPath := stopMidRename(t, s, tasks[0].ID, "Nome novo", true)
	c.Engine = newFake()
	s2 := startService(t, c)
	if got := taskByID(t, s2, tasks[0].ID); got.Title != "Nome novo" || got.CompletedFile.Path != newPath {
		t.Fatal("rename not settled on the new name", got)
	}
	if strings.Contains(savedQueue(t, s2), "renamedFrom") {
		t.Fatal("settled rename still saved as pending")
	}
}

func TestRenameCutShortBeforeTheMoveKeepsTheOldName(t *testing.T) {
	c, s, _, tasks := completedFixture(t, "unmoved")
	title := taskByID(t, s, tasks[0].ID).Title
	oldPath, _ := stopMidRename(t, s, tasks[0].ID, "Nome novo", false)
	c.Engine = newFake()
	s2 := startService(t, c)
	if got := taskByID(t, s2, tasks[0].ID); got.Title != title || got.CompletedFile.Path != oldPath {
		t.Fatal("rename not rolled back to the file's name", got)
	}
	if strings.Contains(savedQueue(t, s2), "renamedFrom") {
		t.Fatal("settled rename still saved as pending")
	}
	if _, err := rename(s2, tasks[0].ID, "Nome novo"); err != nil {
		t.Fatal("rename after recovery", err)
	}
}
