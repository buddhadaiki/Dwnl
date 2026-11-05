package main

import (
	hostwin "dwnl/host/internal/windows"
	"errors"
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The setup installs the Chrome extension beside the app. Chrome loads an
// extension from outside its store only by hand, so the window guides that
// step once and remembers when the extension first reaches the app.
const (
	extensionFolderName = "extension"
	extensionMarker     = "extension-connected"
	secondInstanceID    = "second-instance"
	chromeAppPath       = `Software\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`
)

type extensionStatus struct {
	Folder    string `json:"folder"`
	Connected bool   `json:"connected"`
}

// extensionStatusFor is nil when this build was installed without the
// extension, as in development runs.
func extensionStatusFor(installRoot, stateRoot string) *extensionStatus {
	folder := filepath.Join(installRoot, extensionFolderName)
	manifest := filepath.Join(folder, "manifest.json")
	if hostwin.CheckPath(manifest, false) != nil {
		return nil
	}
	if info, err := os.Stat(manifest); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	_, err := os.Stat(filepath.Join(stateRoot, extensionMarker))
	return &extensionStatus{Folder: folder, Connected: err == nil}
}

func rememberExtensionConnected(stateRoot string) error {
	path := filepath.Join(stateRoot, extensionMarker)
	if err := hostwin.CheckPath(path, true); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString(time.Now().UTC().Format(time.RFC3339) + "\n")
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Pipe requests other than a second app instance come from the Chrome bridge.
func fromExtension(requestID string) bool {
	return requestID != "" && requestID != secondInstanceID
}

func (a *App) extensionStatus() *extensionStatus {
	return extensionStatusFor(filepath.Dir(a.executable), a.stateRoot)
}

func (a *App) extensionConnected() {
	if a.extensionSeen.Load() {
		return
	}
	if rememberExtensionConnected(a.stateRoot) == nil {
		a.extensionSeen.Store(true)
	}
}

// OpenChrome and OpenExtensionFolder are exposed only to the local window,
// never to the extension IPC. Chrome ignores chrome:// addresses on its command
// line, so the window copies the extensions address and this only opens Chrome.
func (a *App) OpenChrome() error {
	chrome, err := chromeExecutable()
	if err != nil {
		return err
	}
	return shellOpen(chrome, "")
}

func (a *App) OpenExtensionFolder() error {
	status := a.extensionStatus()
	if status == nil {
		return errors.New("A extensão não foi instalada junto com este aplicativo")
	}
	return shellOpen(status.Folder, "")
}

func chromeExecutable() (string, error) {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, chromeAppPath, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		path, _, err := key.GetStringValue("")
		key.Close()
		path = strings.Trim(path, `"`)
		if err == nil && hostwin.CheckPath(path, false) == nil {
			return path, nil
		}
	}
	return "", errors.New("Não encontrei o Google Chrome. Abra-o pelo menu Iniciar.")
}
