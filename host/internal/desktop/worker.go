package desktop

import (
	"context"
	"errors"
	"os"
	"time"

	"dwnl/host/internal/contracts"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	hostwin "dwnl/host/internal/windows"
)

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		}
		for {
			s.mu.Lock()
			if s.closed || s.paused || s.blocked || s.readiness == "preparing" {
				s.mu.Unlock()
				break
			}
			index := -1
			for i := range s.tasks {
				if s.tasks[i].Task.State == "queued" {
					index = i
					break
				}
			}
			if index < 0 {
				s.mu.Unlock()
				break
			}
			// This also coordinates with the legacy native host while users migrate.
			lease, busy, err := hostwin.AcquireLease("active-task", false)
			if err != nil || busy {
				s.paused = true
				s.warning = "Outro processo de download está ativo. Aguarde e use Continuar fila."
				if saveErr := s.persist(); saveErr != nil {
					s.persistFailure(saveErr)
				}
				s.mu.Unlock()
				break
			}
			work, cancel := context.WithCancel(s.ctx)
			s.activeID = s.tasks[index].Task.ID
			s.activeCancel = cancel
			s.tasks[index].Task.State = "analyzing"
			s.tasks[index].Task.Progress = nil
			s.tasks[index].Task.Error = ""
			if err = s.persist(); err != nil {
				s.persistFailure(err)
				s.activeID = ""
				s.activeCancel = nil
				cancel()
				lease.Close()
				s.mu.Unlock()
				break
			}
			record := s.tasks[index]
			s.mu.Unlock()
			s.execute(work, record)
			cancel()
			s.mu.Lock()
			// Unconfirmed process termination must retain the cross-process lease for
			// the app lifetime. Closing releases it only as the app itself is exiting.
			blocked := s.blocked
			s.activeID = ""
			s.activeCancel = nil
			if blocked {
				s.retainedLeases = append(s.retainedLeases, lease)
			}
			s.mu.Unlock()
			if !blocked {
				lease.Close()
			}
		}
	}
}

func (s *Service) selectPlan(ctx context.Context, r queuedRecord) (contracts.Analysis, contracts.MediaItem, *contracts.VideoChoice, error) {
	s.mu.Lock()
	a, ok := s.analyses[r.Selection.AnalysisID]
	s.mu.Unlock()
	expires, _ := time.Parse(time.RFC3339, a.ExpiresAt)
	if ok && time.Now().Add(5*time.Second).Before(expires) {
		if identity, err := s.engine.SelectionIdentity(a.AnalysisID, r.Selection.ItemID, r.Selection.VideoOptionID); err == nil && identity == r.Identity {
			for _, item := range a.Items {
				if item.ItemID == r.Selection.ItemID {
					if r.Task.Mode == contracts.ModeMP3 {
						return a, item, nil, nil
					}
					for _, choice := range item.VideoChoices {
						if choice.OptionID == r.Selection.VideoOptionID {
							copy := choice
							return a, item, &copy, nil
						}
					}
				}
			}
		}
	}
	// Expiry/restart refreshes the source; UI positions and titles cannot select
	// a different clip or a lower quality. Require one exact private identity.
	select {
	case s.analyzeSlot <- struct{}{}:
		defer func() { <-s.analyzeSlot }()
	case <-ctx.Done():
		return a, contracts.MediaItem{}, nil, ctx.Err()
	}
	work, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	a, err := s.engine.Analyze(work, r.Task.URL)
	if err != nil {
		return a, contracts.MediaItem{}, nil, err
	}
	a.HostSessionID = s.sessionID
	var selected contracts.MediaItem
	var choice *contracts.VideoChoice
	matches := 0
	for _, item := range a.Items {
		if r.Task.Mode == contracts.ModeMP3 {
			identity, e := s.engine.SelectionIdentity(a.AnalysisID, item.ItemID, "")
			if e == nil && identity == r.Identity {
				selected = item
				matches++
			}
		} else {
			for _, candidate := range item.VideoChoices {
				identity, e := s.engine.SelectionIdentity(a.AnalysisID, item.ItemID, candidate.OptionID)
				if e == nil && identity == r.Identity {
					selected = item
					copy := candidate
					choice = &copy
					matches++
				}
			}
		}
	}
	if matches != 1 {
		return a, contracts.MediaItem{}, nil, errors.New("A fonte ou o formato selecionado mudou. Analise novamente e escolha o formato; nenhuma qualidade alternativa foi baixada.")
	}
	return a, selected, choice, nil
}

func (s *Service) execute(ctx context.Context, r queuedRecord) {
	a, item, choice, err := s.selectPlan(ctx, r)
	var result contracts.CompletedFile
	attempt := hostruntime.NewID()
	preparedTask := false
	journalStarted := false
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		// Explicit recovery restarts from scratch. Only exact, marked private task
		// folders may be removed; final destination files are never cleanup targets.
		if r.AttemptJob != "" {
			if e := s.store.CleanupTask(r.AttemptJob); e != nil && !errors.Is(e, os.ErrNotExist) {
				err = e
			}
		}
	}
	if err == nil {
		err = hostwin.CreateDirectories(r.DestinationPath, false)
	}
	var workDir string
	if err == nil {
		workDir, err = s.store.PrepareTask(attempt)
		preparedTask = err == nil
	}
	snapshot := contracts.JobSnapshot{HostSessionID: s.sessionID, JobID: attempt, StartRequestID: r.Task.RequestID, State: contracts.StateDownloading}
	if err == nil {
		err = s.store.Put(storage.Record{Fingerprint: r.Fingerprint, Snapshot: snapshot})
		journalStarted = err == nil
	}
	if err == nil {
		s.mu.Lock()
		index := s.taskIndex(r.Task.ID)
		s.tasks[index].AttemptSession = s.sessionID
		s.tasks[index].AttemptJob = attempt
		if s.tasks[index].Task.State != "canceling" {
			s.tasks[index].Task.State = "downloading"
		}
		if e := s.persist(); e != nil {
			err = s.persistFailure(e)
		}
		s.mu.Unlock()
	}
	if err == nil {
		plan := contracts.DownloadPlan{RequestID: r.Task.RequestID, HostSessionID: s.sessionID, JobID: attempt, Analysis: a, Item: item, Mode: r.Task.Mode, VideoChoice: choice, MP3BitrateKbps: r.Task.BitrateKbps, DestinationID: r.Selection.DestinationID, DestinationPath: r.DestinationPath, WorkDir: workDir}
		plan.PrepareFinalization = func(proof contracts.PreparedFinalization) error {
			if !hostwin.IsWithin(r.DestinationPath, proof.File.Path) {
				return errors.New("invalid output destination")
			}
			return s.store.PrepareFinalization(s.sessionID, attempt, proof)
		}
		result, err = s.engine.Execute(ctx, plan, func(progress contracts.Progress) {
			switch progress.Stage {
			case contracts.StateDownloading, contracts.StateMuxing, contracts.StateConverting, contracts.StateSaving:
			default:
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			index := s.taskIndex(r.Task.ID)
			if s.closed || s.tasks[index].Task.State == "canceling" {
				return
			}
			copy := progress
			s.tasks[index].Task.Progress = &copy
			s.tasks[index].Task.State = string(progress.Stage)
		})
		if err == nil && (!hostwin.IsWithin(r.DestinationPath, result.Path) || storage.VerifyCompleted(result) != nil) {
			err = &contracts.ProtocolError{Code: contracts.InvalidOutput, Message: "O arquivo final não pôde ser verificado."}
		}
	}
	var protocol *contracts.ProtocolError
	unconfirmed := errors.As(err, &protocol) && protocol.Code == contracts.CancelFailed
	var cleanupErr error
	if preparedTask && !unconfirmed {
		cleanupErr = s.store.CleanupTask(attempt)
	}
	s.mu.Lock()
	index := s.taskIndex(r.Task.ID)
	task := &s.tasks[index].Task
	task.Progress = nil
	task.CompletedFile = nil
	task.Error = ""
	switch {
	case err == nil:
		task.State = "completed"
		task.CompletedFile = &result
	case unconfirmed:
		task.State = "failed"
		task.Error = safeError(err)
		s.blocked = true
		s.paused = true
		s.warning = "Não foi possível confirmar o encerramento do processo. A fila foi pausada."
	case s.closed:
		task.State = "interrupted"
		task.Error = "Download interrompido. Continuar fila reiniciará a tentativa desde o início."
		s.paused = true
	case ctx.Err() != nil && cleanupErr != nil:
		task.State = "failed"
		task.Error = "O cancelamento ocorreu, mas os arquivos temporários não puderam ser removidos."
	case ctx.Err() != nil:
		task.State = "canceled"
	default:
		task.State = "failed"
		task.Error = safeError(err)
	}
	if err != nil && !errors.As(err, &protocol) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		// Only this host-generated selection error is safe to show literally.
		if err.Error() == "A fonte ou o formato selecionado mudou. Analise novamente e escolha o formato; nenhuma qualidade alternativa foi baixada." {
			task.Error = err.Error()
		}
	}
	if cleanupErr != nil && err == nil {
		s.warning = "Download concluído; não foi possível remover todos os arquivos temporários privados."
	}
	snapshot.State = contracts.JobState(task.State)
	snapshot.CompletedFile = task.CompletedFile
	if task.State == "failed" {
		code := contracts.EngineError
		if protocol != nil {
			code = protocol.Code
		}
		snapshot.Error = &contracts.ProtocolError{Code: code, Message: task.Error}
	}
	if journalStarted {
		if e := s.store.Put(storage.Record{Fingerprint: r.Fingerprint, Snapshot: snapshot, TerminalConfirmed: !unconfirmed}); e != nil {
			s.persistFailure(e)
		}
	}
	if e := s.persist(); e != nil {
		s.persistFailure(e)
	}
	s.mu.Unlock()
}

// Caller holds mu. An active task cannot be archived, so its durable ID remains
// present even when older terminal entries are removed during execution.
func (s *Service) taskIndex(id string) int {
	for i := range s.tasks {
		if s.tasks[i].Task.ID == id {
			return i
		}
	}
	panic("active desktop task disappeared")
}
