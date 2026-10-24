package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/desktop"
	"dwnl/host/internal/desktopipc"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"encoding/json"
	"errors"
	win "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const startupName = "Dwnl"

func equalPath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func strictPayload(raw json.RawMessage, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("Pedido inválido")
	}
	return nil
}
func (a *App) dispatch(ctx context.Context, method string, payload json.RawMessage) (any, error) {
	// Wails and Native Messaging share the same allowlist and payload boundaries.
	data, _ := json.Marshal(desktopipc.Request{Version: 2, ID: "desktop-call", Method: method, Payload: payload})
	if _, err := desktopipc.Decode(data); err != nil {
		return nil, err
	}
	switch method {
	case "openApp":
		if err := strictPayload(payload, &struct{}{}); err != nil {
			return nil, err
		}
		if err := a.show(ctx); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	case "settings":
		var p struct {
			StartAtLogin bool `json:"startAtLogin"`
		}
		if err := strictPayload(payload, &p); err != nil {
			return nil, err
		}
		if a.development {
			return nil, errors.New("Inicialização automática indisponível nesta execução de teste")
		}
		if err := a.setStartup(p.StartAtLogin); err != nil {
			return nil, err
		}
		return a.dispatch(ctx, "snapshot", json.RawMessage(`{}`))
	case "openFile", "openFolder":
		var p struct {
			ID string `json:"id"`
		}
		if err := strictPayload(payload, &p); err != nil {
			return nil, err
		}
		if err := a.openCompleted(ctx, p.ID, method == "openFolder"); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	case "snapshot":
		result, err := a.service.Dispatch(ctx, method, payload)
		if err != nil {
			return nil, err
		}
		snapshot, ok := result.(desktop.Snapshot)
		if !ok {
			return nil, errors.New("Resposta inesperada do serviço")
		}
		enabled, err := a.startupEnabled()
		if err != nil {
			return nil, err
		}
		return struct {
			desktop.Snapshot
			StartAtLogin bool `json:"startAtLogin"`
		}{snapshot, enabled}, nil
	default:
		return a.service.Dispatch(ctx, method, payload)
	}
}
func (a *App) startupEnabled() (bool, error) {
	if a.development {
		return false, nil
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(startupName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == `"`+a.executable+`" --background`, nil
}
func (a *App) setStartup(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	expected := `"` + a.executable + `" --background`
	current, _, readErr := key.GetStringValue(startupName)
	if readErr != nil && !errors.Is(readErr, registry.ErrNotExist) {
		return readErr
	}
	if readErr == nil && current != expected {
		return errors.New("Já existe uma entrada Dwnl de outra instalação; ela foi preservada")
	}
	if enabled {
		return key.SetStringValue(startupName, expected)
	}
	if errors.Is(readErr, registry.ErrNotExist) {
		return nil
	}
	return key.DeleteValue(startupName)
}
func (a *App) openCompleted(ctx context.Context, id string, folder bool) error {
	if id == "" {
		return errors.New("Download inválido")
	}
	result, err := a.service.Dispatch(ctx, "snapshot", json.RawMessage(`{}`))
	if err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var state struct {
		Tasks []struct {
			ID    string                   `json:"id"`
			State string                   `json:"state"`
			File  *contracts.CompletedFile `json:"completedFile"`
		} `json:"tasks"`
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return err
	}
	for _, task := range state.Tasks {
		if task.ID != id {
			continue
		}
		if task.State != "completed" || task.File == nil {
			return errors.New("O download ainda não foi concluído")
		}
		if err = hostwin.CheckPath(task.File.Path, false); err != nil {
			return err
		}
		if err = verifyCompletedContext(ctx, *task.File, !folder); err != nil {
			if ctx.Err() != nil {
				return errors.New("A verificação do arquivo demorou demais. Tente novamente.")
			}
			return errors.New("O arquivo mudou ou não está mais disponível")
		}
		path := task.File.Path
		if folder {
			path = filepath.Dir(path)
		} else {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".mp4", ".webm", ".mkv", ".mp3", ".m4a", ".ogg", ".opus":
			default:
				return errors.New("Formato não permitido para abertura")
			}
		}
		operation, _ := win.UTF16PtrFromString("open")
		target, _ := win.UTF16PtrFromString(path)
		if err := ctx.Err(); err != nil {
			return err
		}
		return win.ShellExecute(0, operation, target, nil, nil, win.SW_SHOWNORMAL)
	}
	return errors.New("Download não encontrado")
}
func showStartupError(message string) {
	title, _ := win.UTF16PtrFromString("Dwnl")
	text, _ := win.UTF16PtrFromString(message)
	win.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}

// Opening a folder never hashes a potentially multi-gigabyte file. Opening the
// media itself verifies incrementally and observes cancellation before launching.
func verifyCompletedContext(ctx context.Context, file contracts.CompletedFile, full bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || uint64(info.Size()) != file.SizeBytes {
		return errors.New("Arquivo alterado")
	}
	if !full {
		return nil
	}
	h := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, e := f.Read(buffer)
		if n > 0 {
			h.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(file.SHA256) {
		return errors.New("Hash do arquivo alterado")
	}
	return ctx.Err()
}
