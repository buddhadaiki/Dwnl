package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"dwnl/host/internal/contracts"
	hostruntime "dwnl/host/internal/runtime"
)

func (s *Service) analyze(ctx context.Context, url string) (contracts.Analysis, error) {
	s.mu.Lock()
	preparing := s.readiness == "preparing"
	s.mu.Unlock()
	if preparing {
		return contracts.Analysis{}, errors.New("O aplicativo está preparando os componentes. Aguarde alguns segundos.")
	}
	select {
	case s.analyzeSlot <- struct{}{}:
		defer func() { <-s.analyzeSlot }()
	case <-ctx.Done():
		return contracts.Analysis{}, ctx.Err()
	default:
		return contracts.Analysis{}, errors.New("Já existe uma análise em andamento.")
	}
	work, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	analysis, err := s.engine.Analyze(work, url)
	if err != nil {
		return contracts.Analysis{}, err
	}
	analysis.HostSessionID = s.sessionID
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return contracts.Analysis{}, context.Canceled
	}
	s.analyses[analysis.AnalysisID] = analysis
	s.analysisOrder = append(s.analysisOrder, analysis.AnalysisID)
	if len(s.analysisOrder) > 20 {
		old := s.analysisOrder[0]
		s.analysisOrder = s.analysisOrder[1:]
		delete(s.analyses, old)
		for key, c := range s.cursors {
			if c.analysis == old {
				delete(s.cursors, key)
			}
		}
	}
	wire := analysis
	wire.Items = append([]contracts.MediaItem{}, analysis.Items...)
	for i, item := range wire.Items {
		if len(item.VideoChoices) > 100 {
			item.VideoChoices = append([]contracts.VideoChoice{}, item.VideoChoices[:100]...)
			item.FormatsCursor = hostruntime.NewID()
			item.OmittedMetadata = true
			s.cursors[item.FormatsCursor] = cursor{analysis.AnalysisID, item.ItemID, 100}
		}
		wire.Items[i] = item
	}
	return wire, nil
}
func (s *Service) listFormats(req contracts.Request) (contracts.ListFormatsResult, error) {
	p, _ := contracts.PayloadAs[contracts.ListFormatsPayload](req)
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.analyses[p.AnalysisID]
	if !ok {
		return contracts.ListFormatsResult{}, errors.New("Analise a página novamente.")
	}
	if err := contracts.ValidateRequestContext(req, contracts.ValidationContext{HostSessionID: s.sessionID, Analysis: &a}); err != nil {
		return contracts.ListFormatsResult{}, err
	}
	for _, item := range a.Items {
		if item.ItemID != p.ItemID {
			continue
		}
		offset := 0
		if p.Cursor != "" {
			c, ok := s.cursors[p.Cursor]
			if !ok || c.analysis != p.AnalysisID || c.item != p.ItemID {
				return contracts.ListFormatsResult{}, errors.New("Página de formatos inválida.")
			}
			offset = c.offset
		}
		if offset > len(item.VideoChoices) {
			return contracts.ListFormatsResult{}, errors.New("Página de formatos inválida.")
		}
		end := min(offset+100, len(item.VideoChoices))
		result := contracts.ListFormatsResult{AnalysisID: a.AnalysisID, ItemID: item.ItemID, Choices: append([]contracts.VideoChoice{}, item.VideoChoices[offset:end]...), OmittedMetadata: item.OmittedMetadata}
		if end < len(item.VideoChoices) {
			result.NextCursor = hostruntime.NewID()
			s.cursors[result.NextCursor] = cursor{a.AnalysisID, item.ItemID, end}
		}
		return result, nil
	}
	return contracts.ListFormatsResult{}, errors.New("Item indisponível.")
}

func (s *Service) enqueue(p enqueuePayload) (Task, error) {
	b, _ := json.Marshal(p.StartPayload)
	req, err := s.request(contracts.OperationStart, p.RequestID, b)
	if err != nil {
		return Task{}, err
	}
	key := fingerprint(p.StartPayload)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Durable idempotency is checked before ephemeral analysis and destination
	// lookups, so reconnecting with the original request remains safe after restart.
	for _, r := range s.tasks {
		if r.Task.RequestID == p.RequestID {
			if r.Fingerprint != key {
				return Task{}, errors.New("REQUEST_CONFLICT: este identificador já pertence a outro download.")
			}
			return cloneTask(r.Task), nil
		}
	}
	archived, archiveErr := s.readArchived(p.RequestID)
	if archiveErr != nil {
		return Task{}, archiveErr
	}
	if archived != nil {
		if archived.Fingerprint != key {
			return Task{}, ErrArchiveConflict
		}
		return cloneTask(archived.Task), nil
	}
	if s.blocked {
		return Task{}, errors.New("A fila está bloqueada por um erro de persistência ou encerramento; reinicie o aplicativo após conferir o erro.")
	}
	if s.readiness == "preparing" {
		return Task{}, errors.New("O aplicativo ainda está preparando os componentes.")
	}
	if len(s.tasks) >= maxTasks {
		return Task{}, errors.New("A fila e histórico atingiram o limite de 200 registros. Use Arquivar histórico no aplicativo para liberar espaço sem apagar arquivos ou recibos.")
	}
	a, ok := s.analyses[p.AnalysisID]
	if !ok {
		return Task{}, errors.New("Analise a página novamente antes de adicionar o download.")
	}
	if err = contracts.ValidateRequestContext(req, contracts.ValidationContext{HostSessionID: s.sessionID, Analysis: &a}); err != nil {
		return Task{}, err
	}
	if p.Mode == contracts.ModeVideo && !s.capabilities.Video {
		return Task{}, errors.New("Os componentes de vídeo não estão disponíveis.")
	}
	if p.Mode == contracts.ModeMP3 && !s.capabilities.MP3 {
		return Task{}, errors.New("O codificador MP3 não está disponível.")
	}
	destination, err := s.destinations.Resolve(p.DestinationID)
	if err != nil {
		return Task{}, err
	}
	identity, err := s.engine.SelectionIdentity(p.AnalysisID, p.ItemID, p.VideoOptionID)
	var expired *contracts.ProtocolError
	if errors.As(err, &expired) && expired.Code == contracts.AnalysisExpired {
		// The engine also caches worker re-analyses, so it can drop one we still list.
		return Task{}, errors.New("Analise a página novamente antes de adicionar o download.")
	}
	if err != nil {
		return Task{}, err
	}
	if !digestPattern.MatchString(identity) {
		return Task{}, errors.New("Não foi possível confirmar a identidade da mídia.")
	}
	var item contracts.MediaItem
	for _, candidate := range a.Items {
		if candidate.ItemID == p.ItemID {
			item = candidate
			break
		}
	}
	task := Task{ID: hostruntime.NewID(), RequestID: p.RequestID, URL: a.SourceURL, Title: item.Title, Mode: p.Mode, BitrateKbps: p.BitrateKbps, State: "queued", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	s.tasks = append(s.tasks, queuedRecord{Task: task, Selection: p.StartPayload, Identity: identity, Fingerprint: key, DestinationPath: destination})
	if err = s.persist(); err != nil {
		// The rollback leaves memory equal to the last saved queue, so a rejected
		// addition (size limit, brief sharing violation) must not block it.
		s.tasks = s.tasks[:len(s.tasks)-1]
		return Task{}, fmt.Errorf("Não foi possível salvar a fila: %w", err)
	}
	s.signal()
	return task, nil
}
func (s *Service) cancelTask(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readiness == "preparing" {
		return Task{}, errors.New("Aguarde a recuperação inicial da fila.")
	}
	for i := range s.tasks {
		r := &s.tasks[i]
		if r.Task.ID != id {
			continue
		}
		if terminal(r.Task.State) && r.Task.State != "interrupted" {
			return cloneTask(r.Task), nil
		}
		before := r.Task
		if s.activeID == id {
			r.Task.State = "canceling"
		} else {
			r.Task.State = "canceled"
		}
		r.Task.Progress = nil
		r.Task.Error = ""
		if err := s.persist(); err != nil {
			r.Task = before
			return Task{}, s.persistFailure(err)
		}
		if s.activeID == id && s.activeCancel != nil {
			s.activeCancel()
		}
		return cloneTask(r.Task), nil
	}
	return Task{}, errors.New("Download desconhecido.")
}
func (s *Service) continueQueue() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readiness == "preparing" {
		return Snapshot{}, errors.New("Aguarde a recuperação inicial da fila.")
	}
	if s.blocked {
		return Snapshot{}, errors.New("Não é seguro continuar antes de resolver o erro informado e reiniciar o aplicativo.")
	}
	before := append([]queuedRecord(nil), s.tasks...)
	wasPaused := s.paused
	for i := range s.tasks {
		if s.tasks[i].Task.State == "interrupted" {
			s.tasks[i].Task.State = "queued"
			s.tasks[i].Task.Error = "A tentativa será reiniciada desde o início; arquivos finais existentes serão preservados."
		}
	}
	s.paused = false
	if err := s.persist(); err != nil {
		s.tasks = before
		s.paused = wasPaused
		return Snapshot{}, s.persistFailure(err)
	}
	s.signal()
	return s.snapshotLocked(), nil
}
