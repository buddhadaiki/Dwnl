package storage

import (
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	win "golang.org/x/sys/windows"
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

func validLocalPath(path string) bool {
	if !privateText(path, 4096) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' || !((volume[0] >= 'A' && volume[0] <= 'Z') || (volume[0] >= 'a' && volume[0] <= 'z')) {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, volume+string(os.PathSeparator)), string(os.PathSeparator)) {
		if part != "" && (strings.ContainsAny(part, `<>:"/\|?*`) || strings.TrimRight(part, " .") != part) {
			return false
		}
	}
	return true
}

func verifyPreparedFile(proof contracts.PreparedFinalization) error {
	parent := filepath.Dir(proof.File.Path)
	unlock, err := hostwin.LockPath(parent, false)
	if err != nil {
		return err
	}
	defer unlock()
	id, err := hostwin.IdentifyFile(parent)
	if err != nil {
		return err
	}
	if contracts.PhysicalIdentity(id) != proof.ParentIdentity {
		return errors.New("prepared destination was replaced")
	}
	if err = hostwin.CheckPath(proof.File.Path, false); err != nil {
		return err
	}
	p, err := win.UTF16PtrFromString(proof.File.Path)
	if err != nil {
		return err
	}
	// This handle denies both writes and deletion during all checks. Identity,
	// size and hash are read from this one object, never a reopened pathname.
	h, err := win.CreateFile(p, win.GENERIC_READ|win.FILE_READ_ATTRIBUTES, win.FILE_SHARE_READ, nil, win.OPEN_EXISTING, win.FILE_FLAG_OPEN_REPARSE_POINT|win.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), proof.File.Path)
	if f == nil {
		win.CloseHandle(h)
		return errors.New("invalid prepared file handle")
	}
	defer f.Close()
	var info win.ByHandleFileInformation
	if err = win.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	actual := contracts.PhysicalIdentity{VolumeSerialNumber: info.VolumeSerialNumber, FileIndexHigh: info.FileIndexHigh, FileIndexLow: info.FileIndexLow}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if info.FileAttributes&(win.FILE_ATTRIBUTE_DIRECTORY|win.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || actual != proof.Identity || size != proof.File.SizeBytes {
		return errors.New("prepared file identity or size differs")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != proof.File.SHA256 {
		return errors.New("prepared file hash differs")
	}
	return nil
}
