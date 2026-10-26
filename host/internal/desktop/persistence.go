package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"

	"dwnl/host/internal/contracts"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	hostwin "dwnl/host/internal/windows"
)

const maxQueueBytes = 2 * 1024 * 1024

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var ErrCorruptQueue = errors.New("A fila salva está corrompida. O arquivo original foi preservado; a fila não será reiniciada automaticamente.")

type queueFile struct {
	Version       int                     `json:"version"`
	Owner         string                  `json:"owner"`
	Paused        bool                    `json:"paused"`
	Tasks         []queuedRecord          `json:"tasks"`
	LegacyHistory []contracts.JobSnapshot `json:"legacyHistory,omitempty"`
}

func decode(data []byte, out any) error {
	if len(data) > maxQueueBytes || !utf8.Valid(data) {
		return errors.New("Dados inválidos ou grandes demais.")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("Dados adicionais após JSON.")
	}
	return nil
}
func (s *Service) load() error {
	path := filepath.Join(s.store.Root(), "desktop-queue.json")
	unlock, err := hostwin.LockPath(path, true)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxQueueBytes+1))
	if err != nil {
		return err
	}
	var saved queueFile
	owner, err := hostwin.UserID()
	if err != nil {
		return err
	}
	if decode(b, &saved) != nil || saved.Version != 1 || saved.Owner != owner || saved.Tasks == nil || len(saved.Tasks) > maxTasks {
		return ErrCorruptQueue
	}
	var required map[string]json.RawMessage
	if json.Unmarshal(b, &required) != nil {
		return ErrCorruptQueue
	}
	for _, key := range []string{"version", "owner", "paused", "tasks"} {
		if raw, ok := required[key]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return ErrCorruptQueue
		}
	}
	for _, legacy := range saved.LegacyHistory {
		if validateSavedSnapshot(legacy) != nil {
			return ErrCorruptQueue
		}
	}
	seenID, seenRequest := map[string]bool{}, map[string]bool{}
	for _, r := range saved.Tasks {
		if seenID[r.Task.ID] || seenRequest[r.Task.RequestID] || s.validateQueueRecord(r) != nil {
			return ErrCorruptQueue
		}
		seenID[r.Task.ID] = true
		seenRequest[r.Task.RequestID] = true
	}
	s.legacy = saved.LegacyHistory
	s.tasks = saved.Tasks
	s.paused = saved.Paused
	// No process survives app ownership. Pending work always waits for a new
	// explicit command, even if the previous process died before saving paused.
	for i := range s.tasks {
		t := &s.tasks[i].Task
		if t.State == "canceling" {
			// The user's cancellation outlives the process; never requeue it.
			t.State = "canceled"
			t.Progress = nil
			t.Error = ""
		} else if !terminal(t.State) && t.State != "queued" {
			t.State = "interrupted"
			t.Progress = nil
			t.Error = "Download interrompido. Continuar fila fará uma nova tentativa desde o início."
		}
		if t.State == "queued" || t.State == "interrupted" {
			s.paused = true
		}
	}
	return nil
}

// persist is called under s.mu after initialization. A single per-user lease
// serializes queue writers; the existing private directory guard prevents
// symlink/reparse replacement while the flushed file is atomically replaced.
func (s *Service) persist() error {
	owner, err := hostwin.UserID()
	if err != nil {
		return err
	}
	b, err := json.Marshal(queueFile{Version: 1, Owner: owner, Paused: s.paused, Tasks: s.tasks, LegacyHistory: s.legacy})
	if err != nil {
		return err
	}
	if len(b) > maxQueueBytes {
		return errors.New("A fila atingiu o limite de armazenamento.")
	}
	root := s.store.Root()
	unlock, err := hostwin.LockPath(root, false)
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(root, "desktop-queue.json")
	if err = hostwin.CheckPath(path, true); err != nil {
		return err
	}
	tmp := path + "." + hostruntime.NewID() + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err = file.Write(b); err != nil {
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
	return hostwin.ReplaceFile(tmp, path)
}

func terminal(state string) bool {
	return state == "completed" || state == "failed" || state == "canceled" || state == "interrupted"
}

// Recovery checks the existing physical publication journal before a retry.
// No equal-title or equal-size fallback can turn unconfirmed bytes into success.
func (s *Service) recover() error {
	s.mu.Lock()
	tasks := append([]queuedRecord{}, s.tasks...)
	legacy := append([]contracts.JobSnapshot(nil), s.legacy...)
	s.mu.Unlock()
	records, err := s.store.History()
	if err != nil {
		return err
	}
	ownedRequests := map[string]bool{}
	for i := range tasks {
		r := &tasks[i]
		ownedRequests[r.Task.RequestID] = true
		archived, readErr := s.readArchived(r.Task.RequestID)
		if readErr != nil {
			return readErr
		}
		if archived != nil {
			if !sameArchivedRequest(*r, *archived) {
				return ErrArchiveConflict
			}
			tasks[i] = *archived
			continue
		}
		if r.AttemptJob == "" {
			continue
		}
		var record *storage.Record
		for j := range records {
			candidate := &records[j]
			if candidate.Snapshot.HostSessionID == r.AttemptSession && candidate.Snapshot.JobID == r.AttemptJob {
				record = candidate
				break
			}
		}
		// Completed entries older than the bounded legacy journal retain their
		// durable queue receipt; they are history, never automatically redownloaded.
		if record == nil {
			if r.Task.State == "interrupted" || r.Task.State == "queued" {
				r.Task.State = "failed"
				r.Task.Progress = nil
				r.Task.Error = "O registro da tentativa anterior não está disponível. Confira a pasta antes de criar um novo download."
			}
			continue
		}
		if r.Task.State == "completed" {
			continue
		}
		if record.PreparedFinalization != nil {
			complete, verified, e := s.store.PreparedCompletion(*record)
			if e != nil {
				return e
			}
			if verified {
				r.Task.State = "completed"
				r.Task.CompletedFile = &complete
				r.Task.Error = ""
				r.Task.Progress = nil
				continue
			}
			// A prepared publication may have happened but can no longer be proved.
			// Refuse to redownload it, avoiding duplicate output after tampering/loss.
			r.Task.State = "failed"
			r.Task.CompletedFile = nil
			r.Task.Progress = nil
			r.Task.Error = "A publicação anterior não pôde ser confirmada. Confira a pasta antes de criar um novo download."
			continue
		}
		if record.TerminalConfirmed {
			switch record.Snapshot.State {
			case contracts.StateCompleted:
				if record.Snapshot.CompletedFile != nil && storage.VerifyCompleted(*record.Snapshot.CompletedFile) == nil {
					r.Task.State = "completed"
					r.Task.CompletedFile = record.Snapshot.CompletedFile
					r.Task.Error = ""
				} else {
					r.Task.State = "failed"
					r.Task.CompletedFile = nil
					r.Task.Error = "O arquivo concluído anteriormente não pôde ser verificado."
				}
			case contracts.StateCanceled:
				r.Task.State = "canceled"
				r.Task.Error = ""
			case contracts.StateFailed:
				// The journal keeps only a "Task ended with CODE" placeholder; the
				// queue saved the user-facing message right after that entry.
				if r.Task.State != "failed" || r.Task.Error == "" {
					r.Task.Error = "O download falhou antes do reinício do aplicativo."
					if record.Snapshot.Error != nil {
						r.Task.Error = string(record.Snapshot.Error.Code) + ": " + r.Task.Error
					}
				}
				r.Task.State = "failed"
			}
		}
	}
	// Ownership belongs to a request, not one execution attempt: retries receive
	// new job IDs, but every prior attempt remains part of the same desktop task.
	// Apply this to persisted legacy entries too, repairing earlier projections.
	candidates := append([]contracts.JobSnapshot{}, legacy...)
	for _, record := range records {
		candidates = append(candidates, record.Snapshot)
	}
	legacy = nil
	seenLegacy := map[string]bool{}
	checkedArchive := map[string]bool{}
	for _, candidate := range candidates {
		requestID := candidate.StartRequestID
		if ownedRequests[requestID] {
			continue
		}
		if !checkedArchive[requestID] {
			archived, readErr := s.readArchived(requestID)
			if readErr != nil {
				return readErr
			}
			checkedArchive[requestID] = true
			ownedRequests[requestID] = archived != nil
		}
		if ownedRequests[requestID] {
			continue
		}
		key := candidate.HostSessionID + ":" + candidate.JobID
		if seenLegacy[key] {
			continue
		}
		seenLegacy[key] = true
		if !terminal(string(candidate.State)) {
			candidate.State = contracts.StateInterrupted
			candidate.Progress = nil
			candidate.Error = &contracts.ProtocolError{Code: contracts.EngineError, Message: "Tentativa anterior interrompida. Este registro é somente histórico; nenhum download está em execução."}
		}
		legacy = append(legacy, candidate)
	}
	s.mu.Lock()
	s.tasks = tasks
	s.legacy = legacy
	s.mu.Unlock()
	return nil
}

func (s *Service) persistFailure(err error) error {
	s.paused = true
	s.blocked = true
	s.warning = "A fila foi pausada para preservar os downloads: " + safeError(err)
	return fmt.Errorf("Não foi possível salvar a fila: %w", err)
}

func validateSavedSnapshot(snapshot contracts.JobSnapshot) error {
	body, err := json.Marshal(contracts.StatusResult{Snapshot: &snapshot})
	if err != nil {
		return err
	}
	data, err := json.Marshal(contracts.Response{ProtocolVersion: 1, Kind: "response", RequestID: "queue-check", HostSessionID: snapshot.HostSessionID, Operation: contracts.OperationStatus, Result: body})
	if err != nil {
		return err
	}
	_, err = contracts.DecodeMessage(data)
	return err
}

func (s *Service) validateQueueRecord(r queuedRecord) error {
	var err error

	if !idPattern.MatchString(r.Task.ID) || !idPattern.MatchString(r.Task.RequestID) || !digestPattern.MatchString(r.Identity) || r.Fingerprint != fingerprint(r.Selection) || !filepath.IsAbs(r.DestinationPath) {
		return ErrCorruptQueue
	}
	if _, e := time.Parse(time.RFC3339, r.Task.CreatedAt); e != nil {
		return ErrCorruptQueue
	}
	if r.Task.CompletedFile != nil {
		if !hostwin.IsWithin(r.DestinationPath, r.Task.CompletedFile.Path) || validateSavedSnapshot(contracts.JobSnapshot{HostSessionID: "queue", JobID: r.Task.ID, StartRequestID: r.Task.RequestID, State: contracts.StateCompleted, CompletedFile: r.Task.CompletedFile}) != nil {
			return ErrCorruptQueue
		}
	}
	if r.Task.Mode != r.Selection.Mode || r.Task.BitrateKbps != r.Selection.BitrateKbps || r.Task.Title == "" || len(r.Task.Title) > 4096 || len(r.Task.CreatedAt) > 64 {
		return ErrCorruptQueue
	}
	b, _ := json.Marshal(contracts.AnalyzePayload{URL: r.Task.URL})
	if _, err = s.request(contracts.OperationAnalyze, "validate", b); err != nil {
		return ErrCorruptQueue
	}
	b, _ = json.Marshal(r.Selection)
	if _, err = s.request(contracts.OperationStart, r.Task.RequestID, b); err != nil {
		return ErrCorruptQueue
	}
	if (r.AttemptSession == "") != (r.AttemptJob == "") || (r.AttemptSession != "" && (!idPattern.MatchString(r.AttemptSession) || !idPattern.MatchString(r.AttemptJob))) {
		return ErrCorruptQueue
	}
	switch r.Task.State {
	case "queued", "analyzing", "ready", "downloading", "muxing", "converting", "saving", "canceling", "completed", "canceled", "failed", "interrupted":
	default:
		return ErrCorruptQueue
	}
	if r.Task.State == "completed" && r.Task.CompletedFile == nil {
		return ErrCorruptQueue
	}
	if r.Task.State != "completed" && r.Task.CompletedFile != nil {
		return ErrCorruptQueue
	}
	return nil
}
