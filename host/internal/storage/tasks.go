package storage

import (
	"crypto/rand"
	"dwnl/host/internal/windows"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type taskMark struct {
	Version int    `json:"version"`
	Owner   string `json:"owner"`
	JobID   string `json:"jobId"`
}

func (s *Store) PrepareTask(jobID string) (dir string, err error) {
	if !taskIDPattern.MatchString(jobID) {
		return "", errors.New("invalid task identity")
	}
	err = s.locked(func() error {
		if _, e := s.read(); e != nil {
			return e
		}
		if e := s.ensure(); e != nil {
			return e
		}
		tasks := filepath.Join(s.root, "tasks")
		if e := windows.CreateDirectories(tasks, true); e != nil {
			return e
		}
		dir = filepath.Join(tasks, jobID)
		if e := os.Mkdir(dir, 0700); e != nil {
			return e
		}
		return atomicJSON(filepath.Join(dir, ".dwnl-task.json"), taskMark{1, s.owner, jobID})
	})
	return
}
func (s *Store) CleanupTask(jobID string) error {
	if !taskIDPattern.MatchString(jobID) {
		return errors.New("invalid task identity")
	}
	dir := filepath.Join(s.root, "tasks", jobID)
	unlock, err := windows.LockPath(filepath.Dir(dir), false)
	if err != nil {
		return err
	}
	defer unlock()
	return windows.RemoveOwnedTree(dir, func() error {
		// Validate the marker while the task directory is pinned against replacement.
		if err := windows.CheckPath(filepath.Join(dir, ".dwnl-task.json"), false); err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(dir, ".dwnl-task.json"))
		if err != nil {
			return err
		}
		var mark taskMark
		if strictJSON(b, &mark) != nil || mark.Version != 1 || mark.Owner != s.owner || mark.JobID != jobID {
			return errors.New("task ownership is unconfirmed")
		}
		return nil
	})
}

// OwnedIntermediate is an exact file created in an approved destination. Its
// identity prevents cleanup from deleting a user's replacement at the same name.
type OwnedIntermediate struct {
	Path           string
	Identity       windows.FileIdentity
	ParentIdentity windows.FileIdentity
}

func CreateIntermediate(destination, jobID string) (*os.File, OwnedIntermediate, error) {
	if !taskIDPattern.MatchString(jobID) {
		return nil, OwnedIntermediate{}, errors.New("invalid task identity")
	}
	unlock, err := windows.LockPath(destination, false)
	if err != nil {
		return nil, OwnedIntermediate{}, err
	}
	defer unlock()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, OwnedIntermediate{}, err
	}
	path := filepath.Join(destination, ".dwnl-"+jobID+"-"+hex.EncodeToString(random[:])+".partial")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, OwnedIntermediate{}, err
	}
	id, err := windows.IdentifyFile(path)
	if err != nil {
		f.Close()
		return nil, OwnedIntermediate{}, err
	}
	parentID, err := windows.IdentifyFile(destination)
	if err != nil {
		f.Close()
		return nil, OwnedIntermediate{}, err
	}
	return f, OwnedIntermediate{Path: path, Identity: id, ParentIdentity: parentID}, nil
}
func (o OwnedIntermediate) Cleanup() error { return windows.RemoveExactFile(o.Path, o.Identity) }

// Publish moves only this owned intermediate to a new name in its approved
// destination. The writing file must be closed before publication.
func (o OwnedIntermediate) Publish(target string) error {
	return windows.PublishExactFile(o.Path, target, o.Identity, o.ParentIdentity)
}
