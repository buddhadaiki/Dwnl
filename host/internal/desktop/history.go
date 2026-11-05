package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"dwnl/host/internal/contracts"
	"dwnl/host/internal/engine"
	hostwin "dwnl/host/internal/windows"
)

const maxTitleRunes = 120

var ErrNameTaken = errors.New("Já existe um arquivo com esse nome na pasta. Escolha outro nome.")
var errFileUnavailable = errors.New("O arquivo mudou ou não está mais na pasta.")
var errFileInUse = errors.New("Não foi possível renomear o arquivo. Feche-o em outros programas e tente novamente.")

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > maxTitleRunes {
		return "", fmt.Errorf("Digite um nome com até %d caracteres.", maxTitleRunes)
	}
	for _, r := range title {
		if unicode.IsControl(r) {
			return "", errors.New("O nome tem caracteres que não podem ser usados.")
		}
	}
	return title, nil
}

func (s *Service) findTask(id string) int {
	for i := range s.tasks {
		if s.tasks[i].Task.ID == id {
			return i
		}
	}
	return -1
}

// renameTask gives a completed download a new title and moves its file to the
// matching name in the same folder. It never replaces another file.
func (s *Service) renameTask(id, title string) (Task, error) {
	title, err := cleanTitle(title)
	if err != nil {
		return Task{}, err
	}
	// Archiving copies whole records into receipts; a rename between that copy
	// and the queue update would leave a receipt that no longer matches.
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.mu.Lock()
	if s.readiness == "preparing" {
		s.mu.Unlock()
		return Task{}, errors.New("Aguarde a recuperação inicial do histórico.")
	}
	if s.blocked {
		s.mu.Unlock()
		return Task{}, errors.New("Resolva o erro de armazenamento e reinicie o aplicativo antes de renomear.")
	}
	i := s.findTask(id)
	if i < 0 {
		s.mu.Unlock()
		return Task{}, errors.New("Download desconhecido.")
	}
	before := s.tasks[i]
	active := s.activeID == id
	s.mu.Unlock()
	if active || before.Task.State != "completed" || before.Task.CompletedFile == nil {
		return Task{}, errors.New("Só downloads concluídos podem ser renomeados.")
	}
	if before.Task.Title == title {
		return cloneTask(before.Task), nil
	}
	if archived, err := s.readArchived(before.Task.RequestID); err != nil {
		return Task{}, err
	} else if archived != nil {
		return Task{}, errors.New("Este download já foi arquivado. Reinicie o aplicativo para atualizar a lista.")
	}
	oldPath := before.Task.CompletedFile.Path
	newPath := filepath.Join(filepath.Dir(oldPath), engine.SanitizeTitle(title)+filepath.Ext(oldPath))
	// A name that differs only in letter case is the same file on Windows; it
	// keeps its name and only the title changes.
	moved := !strings.EqualFold(oldPath, newPath)
	if !moved {
		newPath = oldPath
	}
	after := before
	after.Task.Title = title
	file := *before.Task.CompletedFile
	file.Path = newPath
	after.Task.CompletedFile = &file
	if !moved {
		if err = s.replaceRecord(id, before, after); err != nil {
			return Task{}, err
		}
		return cloneTask(after.Task), nil
	}
	// The queue saves the rename before the file moves. If the app stops in
	// between, the next start keeps whichever name holds the file (load).
	pending := after
	pending.RenamedFrom = &renameOrigin{Title: before.Task.Title, Path: oldPath}
	if err = s.replaceRecord(id, before, pending); err != nil {
		return Task{}, err
	}
	if err = moveCompleted(*before.Task.CompletedFile, newPath); err != nil {
		// Should this save fail too, the saved origin still restores the old
		// name at the next start, because the file never left it.
		_ = s.replaceRecord(id, pending, before)
		return Task{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if i = s.findTask(id); i >= 0 && recordsEqual(s.tasks[i], pending) {
		s.tasks[i] = after
		if err = s.persist(); err != nil {
			// The file already has its new name, which the saved origin keeps at
			// the next start; only the queue stops until storage works again.
			s.persistFailure(err)
		}
	}
	return cloneTask(after.Task), nil
}

// replaceRecord swaps one queue record for another and saves the queue,
// refusing when the record is no longer the one the caller started from.
func (s *Service) replaceRecord(id string, from, to queuedRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.findTask(id)
	if i < 0 || !recordsEqual(s.tasks[i], from) {
		return errors.New("O download mudou durante a renomeação. Tente novamente.")
	}
	if s.validateQueueRecord(to) != nil {
		return errors.New("Esse nome não pode ser usado.")
	}
	s.tasks[i] = to
	if err := s.persist(); err != nil {
		s.tasks[i] = from
		return fmt.Errorf("Não foi possível salvar o novo nome: %w.", err)
	}
	return nil
}

// renameOrigin is saved with a completed record only while its file moves to
// the new name, so a rename cut short by the app stopping can be settled.
type renameOrigin struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

// settleRename finishes a rename the previous process left between saving the
// queue and moving the file: the old name and title come back only when the
// file is still there and not at the new name.
func (r *queuedRecord) settleRename() {
	origin := r.RenamedFrom
	if origin == nil {
		return
	}
	r.RenamedFrom = nil
	if publishedMatches(*r.Task.CompletedFile) {
		return
	}
	old := *r.Task.CompletedFile
	old.Path = origin.Path
	if publishedMatches(old) {
		r.Task.Title = origin.Title
		r.Task.CompletedFile = &old
	}
}

// moveCompleted renames exactly the file the record describes. It proves the
// bytes still match the recorded SHA-256 and keeps them unwritable until the
// rename, which goes by that file's identity, never a pathname looked up again.
func moveCompleted(file contracts.CompletedFile, target string) error {
	if hostwin.CheckPath(file.Path, false) != nil {
		return errFileUnavailable
	}
	identity, release, err := hostwin.HoldVerifiedFile(file.Path, file.SizeBytes, file.SHA256)
	if errors.Is(err, hostwin.ErrFileInUse) {
		return errFileInUse
	}
	if err != nil {
		return errFileUnavailable
	}
	defer release()
	parent, err := hostwin.IdentifyFile(filepath.Dir(file.Path))
	if err != nil {
		return errFileUnavailable
	}
	err = hostwin.PublishExactFile(file.Path, target, identity, parent)
	if errors.Is(err, os.ErrExist) {
		return ErrNameTaken
	}
	if err != nil {
		return errFileInUse
	}
	return nil
}
