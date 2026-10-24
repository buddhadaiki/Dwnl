package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	hostruntime "dwnl/host/internal/runtime"
	hostwin "dwnl/host/internal/windows"
)

const maxArchiveBytes = 128 * 1024

var ErrCorruptArchive = errors.New("O recibo arquivado está corrompido ou pertence a outro usuário. Ele foi preservado; o download não será duplicado.")
var ErrArchiveConflict = errors.New("REQUEST_CONFLICT: há um recibo diferente para este identificador. Os registros foram preservados.")

type archiveFile struct {
	Version int          `json:"version"`
	Owner   string       `json:"owner"`
	Record  queuedRecord `json:"record"`
}
type ArchiveResult struct {
	Archived int `json:"archived"`
}

func archivable(state string) bool {
	return state == "completed" || state == "canceled" || state == "failed"
}
func (s *Service) archivePath(requestID string) (string, error) {
	if !idPattern.MatchString(requestID) {
		return "", ErrCorruptArchive
	}
	// NTFS is case-insensitive; hashing preserves distinct request IDs such as
	// "A" and "a" and avoids reserved device names without changing semantics.
	sum := sha256.Sum256([]byte(requestID))
	return filepath.Join(s.store.Root(), "desktop-history", hex.EncodeToString(sum[:])+".json"), nil
}
func (s *Service) readArchived(requestID string) (*queuedRecord, error) {
	path, err := s.archivePath(requestID)
	if err != nil {
		return nil, err
	}
	unlock, err := hostwin.LockPath(path, true)
	if err != nil {
		return nil, err
	}
	defer unlock()
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	var saved archiveFile
	owner, err := hostwin.UserID()
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveBytes || decode(data, &saved) != nil || saved.Version != 1 || saved.Owner != owner || saved.Record.Task.RequestID != requestID || !archivable(saved.Record.Task.State) || s.validateQueueRecord(saved.Record) != nil {
		return nil, ErrCorruptArchive
	}
	return &saved.Record, nil
}
func recordsEqual(a, b queuedRecord) bool {
	first, e1 := json.Marshal(a)
	second, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(first, second)
}
func sameArchivedRequest(a, b queuedRecord) bool {
	// A crash can leave the prior queue beside a newer terminal receipt. Restore
	// that exact receipt only when all immutable request/attempt fields agree.
	for _, r := range []*queuedRecord{&a, &b} {
		r.Task.State = ""
		r.Task.Error = ""
		r.Task.Progress = nil
		r.Task.CompletedFile = nil
	}
	return recordsEqual(a, b)
}
func syncArchive(path string) error {
	unlock, err := hostwin.LockPath(path, false)
	if err != nil {
		return err
	}
	defer unlock()
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
func (s *Service) writeArchived(record queuedRecord) error {
	if !archivable(record.Task.State) || s.validateQueueRecord(record) != nil {
		return ErrCorruptArchive
	}
	path, err := s.archivePath(record.Task.RequestID)
	if err != nil {
		return err
	}
	if existing, e := s.readArchived(record.Task.RequestID); e != nil {
		return e
	} else if existing != nil {
		if !recordsEqual(*existing, record) {
			return ErrArchiveConflict
		}
		return syncArchive(path)
	}
	owner, err := hostwin.UserID()
	if err != nil {
		return err
	}
	data, err := json.Marshal(archiveFile{Version: 1, Owner: owner, Record: record})
	if err != nil {
		return err
	}
	if len(data) > maxArchiveBytes {
		return ErrCorruptArchive
	}
	parent := filepath.Dir(path)
	if err = hostwin.CreateDirectories(parent, true); err != nil {
		return err
	}
	unlock, err := hostwin.LockPath(parent, false)
	if err != nil {
		return err
	}
	defer unlock()
	temp := filepath.Join(parent, "receipt-"+hostruntime.NewID()+".tmp")
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	identity, err := hostwin.IdentifyFile(temp)
	if err != nil {
		return err
	}
	parentIdentity, err := hostwin.IdentifyFile(parent)
	if err != nil {
		return err
	}
	// PublishExactFile never replaces an existing name. A collision is reconciled
	// by reading its entire validated receipt, never by overwriting differences.
	if err = hostwin.PublishExactFile(temp, path, identity, parentIdentity); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	saved, err := s.readArchived(record.Task.RequestID)
	if err != nil {
		return err
	}
	if saved == nil || !recordsEqual(*saved, record) {
		return ErrArchiveConflict
	}
	return syncArchive(path)
}
func (s *Service) archiveHistory(ctx context.Context) (ArchiveResult, error) {
	s.mu.Lock()
	if s.readiness == "preparing" {
		s.mu.Unlock()
		return ArchiveResult{}, errors.New("Aguarde a recuperação inicial do histórico.")
	}
	if s.blocked {
		s.mu.Unlock()
		return ArchiveResult{}, errors.New("Resolva o erro de armazenamento e reinicie o aplicativo antes de arquivar o histórico.")
	}
	var candidates []queuedRecord
	for _, record := range s.tasks {
		if archivable(record.Task.State) && record.Task.ID != s.activeID {
			candidates = append(candidates, record)
		}
	}
	s.mu.Unlock()
	// Receipt fsyncs run without s.mu so snapshots and engine progress never
	// wait on disk. Only records still unchanged afterwards leave the queue.
	written := map[string]queuedRecord{}
	for _, record := range candidates {
		if err := ctx.Err(); err != nil {
			return ArchiveResult{}, err
		}
		if err := s.writeArchived(record); err != nil {
			return ArchiveResult{}, err
		}
		written[record.Task.ID] = record
	}
	if len(written) == 0 {
		return ArchiveResult{}, nil
	}
	if err := ctx.Err(); err != nil {
		return ArchiveResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked {
		return ArchiveResult{}, errors.New("Resolva o erro de armazenamento e reinicie o aplicativo antes de arquivar o histórico.")
	}
	kept := make([]queuedRecord, 0, len(s.tasks))
	archived := 0
	for _, record := range s.tasks {
		if saved, ok := written[record.Task.ID]; ok && recordsEqual(saved, record) {
			archived++
			continue
		}
		kept = append(kept, record)
	}
	if archived == 0 {
		return ArchiveResult{}, nil
	}
	before := s.tasks
	s.tasks = kept
	if err := s.persist(); err != nil {
		s.tasks = before
		return ArchiveResult{}, s.persistFailure(err)
	}
	return ArchiveResult{Archived: archived}, nil
}
