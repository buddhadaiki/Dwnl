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
// step and records when the extension last reached the app.
const (
	extensionFolderName = "extension"
	extensionMarker     = "extension-connected"
	secondInstanceID    = "second-instance"
	developmentCheckID  = "development-check" // dwnl-desktop-check
	chromeAppPath       = `Software\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`
)

type extensionStatus struct {
	Folder string `json:"folder"`
	// Connected is true once the extension reached the app in this run. A
	// contact from an earlier run is only LastSeen: the extension may have
	// been removed from Chrome since, and nothing tells the app.
	Connected bool `json:"connected"`
	// LastSeen is the last run's first contact, RFC 3339 in UTC; empty never.
	LastSeen string `json:"lastSeen,omitempty"`
}

// extensionStatusFor is nil when this build was installed without the
// extension, as in development runs.
func extensionStatusFor(installRoot, stateRoot string, connected bool) *extensionStatus {
	folder := filepath.Join(installRoot, extensionFolderName)
	manifest := filepath.Join(folder, "manifest.json")
	if hostwin.CheckPath(manifest, false) != nil {
		return nil
	}
	if info, err := os.Stat(manifest); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	return &extensionStatus{Folder: folder, Connected: connected, LastSeen: lastExtensionContact(stateRoot)}
}

func lastExtensionContact(stateRoot string) string {
	path := filepath.Join(stateRoot, extensionMarker)
	if hostwin.CheckPath(path, false) != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 64 {
		return ""
	}
	seen, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return ""
	}
	return seen.UTC().Format(time.RFC3339)
}

// rememberExtensionConnected replaces the recorded contact with now.
func rememberExtensionConnected(stateRoot string, now time.Time) error {
	path := filepath.Join(stateRoot, extensionMarker)
	if err := hostwin.CheckPath(path, true); err != nil {
		return err
	}
	f, err := os.CreateTemp(stateRoot, extensionMarker+"-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.WriteString(now.UTC().Format(time.RFC3339) + "\n")
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = hostwin.ReplaceFile(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// Pipe requests other than a second app instance or the development check
// come from the Chrome bridge.
func fromExtension(requestID string) bool {
	return requestID != "" && requestID != secondInstanceID && requestID != developmentCheckID
}

func (a *App) extensionStatus() *extensionStatus {
	return extensionStatusFor(filepath.Dir(a.executable), a.stateRoot, a.extensionSeen.Load())
}

// extensionConnected records the first contact of each run. The record only
// dates the contact, so a failed write still counts as connected now.
func (a *App) extensionConnected() {
	if a.extensionSeen.CompareAndSwap(false, true) {
		_ = rememberExtensionConnected(a.stateRoot, time.Now())
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
