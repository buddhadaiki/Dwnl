package storage

import (
	"dwnl/host/internal/contracts"
	"errors"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (s *Store) PrepareFinalization(session, job string, proof contracts.PreparedFinalization) error {
	if !taskIDPattern.MatchString(session) || !taskIDPattern.MatchString(job) || validatePreparation(proof) != nil {
		return ErrCorrupt
	}
	return s.locked(func() error {
		l, err := s.read()
		if err != nil {
			return err
		}
		for i := range l.Records {
			r := &l.Records[i]
			if r.Snapshot.HostSessionID != session || r.Snapshot.JobID != job {
				continue
			}
			if r.TerminalConfirmed || r.Snapshot.CompletedFile != nil || r.Snapshot.State == contracts.StateCanceled || r.Snapshot.State == contracts.StateFailed || r.Snapshot.State == contracts.StateInterrupted {
				return errors.New("task is no longer preparing publication")
			}
			// Collision retries may choose another name for this same owned file.
			if old := r.PreparedFinalization; old != nil && (old.Identity != proof.Identity || old.ParentIdentity != proof.ParentIdentity || !samePreparedMedia(old.File, proof.File)) {
				return ErrCorrupt
			}
			r.PreparedFinalization = &proof
			if err = validateRecord(*r); err != nil {
				return err
			}
			if err = s.ensure(); err != nil {
				return err
			}
			return atomicJSON(filepath.Join(s.root, "history.json"), l)
		}
		return errors.New("preparation requires an existing task")
	})
}

func (s *Store) PreparedCompletion(record Record) (contracts.CompletedFile, bool, error) {
	if validateRecord(record) != nil {
		return contracts.CompletedFile{}, false, ErrCorrupt
	}
	proof := record.PreparedFinalization
	if proof == nil {
		return contracts.CompletedFile{}, false, nil
	}
	// Fail closed for filesystem differences. Recovery is a read-only query,
	// and an inaccessible or altered candidate cannot establish completion.
	if verifyPreparedFile(*proof) != nil {
		return contracts.CompletedFile{}, false, nil
	}
	return proof.File, true, nil
}

func samePreparedMedia(a, b contracts.CompletedFile) bool {
	a.Path, b.Path = "", ""
	return a == b
}

func validatePreparation(proof contracts.PreparedFinalization) error {
	if !validLocalPath(proof.File.Path) || proof.Identity.FileIndexHigh|proof.Identity.FileIndexLow == 0 || proof.ParentIdentity.FileIndexHigh|proof.ParentIdentity.FileIndexLow == 0 || proof.Identity.VolumeSerialNumber != proof.ParentIdentity.VolumeSerialNumber {
		return ErrCorrupt
	}
	snapshot := contracts.JobSnapshot{HostSessionID: "journal-check", JobID: "journal-check", StartRequestID: "journal-check", State: contracts.StateCompleted, CompletedFile: &proof.File}
	return validatePrivateSnapshot(snapshot)
}

func privateText(value string, max int) bool {
	if !utf8.ValidString(value) || len(value) == 0 || len(value) > max || strings.TrimFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }) == "" {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
