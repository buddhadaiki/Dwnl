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
	} else if err = moveCompleted(*before.Task.CompletedFile, newPath); err != nil {
		return Task{}, err
	}
	after := before
	after.Task.Title = title
	file := *before.Task.CompletedFile
	file.Path = newPath
	after.Task.CompletedFile = &file
	s.mu.Lock()
	defer s.mu.Unlock()
	undo := func(cause error) (Task, error) {
		if moved {
			if err := moveCompleted(file, oldPath); err != nil {
				return Task{}, fmt.Errorf("%w O arquivo ficou com o nome %q.", cause, filepath.Base(newPath))
			}
		}
		return Task{}, cause
	}
	i = s.findTask(id)
	if i < 0 || !recordsEqual(s.tasks[i], before) {
		return undo(errors.New("O download mudou durante a renomeação. Tente novamente."))
	}
	if s.validateQueueRecord(after) != nil {
		return undo(errors.New("Esse nome não pode ser usado."))
	}
	s.tasks[i] = after
	if err = s.persist(); err != nil {
		s.tasks[i] = before
		return undo(fmt.Errorf("Não foi possível salvar o novo nome: %w.", err))
	}
	return cloneTask(after.Task), nil
}

// moveCompleted renames exactly the file the record describes. A file that
// was replaced or resized since the download is left alone.
func moveCompleted(file contracts.CompletedFile, target string) error {
	if hostwin.CheckPath(file.Path, false) != nil {
		return errFileUnavailable
	}
	info, err := os.Lstat(file.Path)
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != file.SizeBytes {
		return errFileUnavailable
	}
	identity, err := hostwin.IdentifyFile(file.Path)
	if err != nil {
		return errFileUnavailable
	}
	parent, err := hostwin.IdentifyFile(filepath.Dir(file.Path))
	if err != nil {
		return errFileUnavailable
	}
	err = hostwin.PublishExactFile(file.Path, target, identity, parent)
	if errors.Is(err, os.ErrExist) {
		return ErrNameTaken
	}
	if err != nil {
		return errors.New("Não foi possível renomear o arquivo. Feche-o em outros programas e tente novamente.")
	}
	return nil
}
