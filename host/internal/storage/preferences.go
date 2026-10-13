package storage

import (
	hostwin "dwnl/host/internal/windows"
	"errors"
	"os"
	"path/filepath"
)

type destinationPreference struct {
	Version         int    `json:"version"`
	Owner           string `json:"owner"`
	DestinationPath string `json:"destinationPath"`
}

func (s *Store) SaveDestinationPreference(approvedPath string) error {
	if !validLocalPath(approvedPath) {
		return errors.New("destination must be an absolute local path")
	}
	unlock, err := hostwin.LockPath(approvedPath, false)
	if err != nil {
		return err
	}
	defer unlock()
	if err = checkDestination(approvedPath, false); err != nil {
		return err
	}
	return s.locked(func() error {
		if _, err := s.read(); err != nil {
			return err
		}
		if _, err := s.readPreference(); err != nil {
			return err
		}
		if err := s.ensure(); err != nil {
			return err
		}
		return atomicJSON(filepath.Join(s.root, "preferences.json"), destinationPreference{Version: 1, Owner: s.owner, DestinationPath: approvedPath})
	})
}

// A vanished or unsafe old folder falls back without mutating the saved choice.
// Corrupt private configuration returns the safe fallback with an explicit error.
func (s *Store) PreferredDestination(fallback string) (chosen string, err error) {
	if err = checkDestination(fallback, true); err != nil {
		return "", err
	}
	chosen = fallback
	err = s.locked(func() error {
		if _, err := s.read(); err != nil {
			return err
		}
		preference, err := s.readPreference()
		if err != nil || preference == nil {
			return err
		}
		if checkDestination(preference.DestinationPath, false) == nil {
			chosen = preference.DestinationPath
		}
		return nil
	})
	return
}

func checkDestination(path string, allowMissing bool) error {
	if !validLocalPath(path) {
		return errors.New("destination must be an absolute local path")
	}
	unlock, err := hostwin.LockPath(path, allowMissing)
	if err != nil {
		return err
	}
	defer unlock()
	info, err := os.Stat(path)
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("destination is not a directory")
	}
	return nil
}

func (s *Store) readPreference() (*destinationPreference, error) {
	path := filepath.Join(s.root, "preferences.json")
	unlock, err := hostwin.LockPath(path, true)
	if err != nil {
		return nil, ErrCorrupt
	}
	defer unlock()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var preference destinationPreference
	if len(data) > 8192 || strictJSON(data, &preference) != nil || preference.Version != 1 || preference.Owner != s.owner || !validLocalPath(preference.DestinationPath) {
		return nil, ErrCorrupt
	}
	return &preference, nil
}
