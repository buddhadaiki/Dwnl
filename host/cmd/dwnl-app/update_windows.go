package main

import (
	"context"
	"dwnl/host/internal/desktop"
	"dwnl/host/internal/update"
	hostwin "dwnl/host/internal/windows"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type updateInfo struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	Page       string `json:"page"`
	CanInstall bool   `json:"canInstall"`
	// Why the app cannot install it itself; empty when it can.
	Blocked string `json:"blocked,omitempty"`
}

func userAgent() string { return "Dwnl/" + desktop.Version }

// CheckForUpdate, InstallUpdate and OpenUpdatePage are exposed only to the
// local window, never to the extension IPC. Nothing is checked automatically:
// GitHub is contacted only when the person clicks.
func (a *App) CheckForUpdate() (updateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	release, err := a.updates.Latest(ctx, userAgent())
	if err != nil {
		return updateInfo{}, err
	}
	newer, err := update.Newer(release.Version, desktop.Version)
	if err != nil {
		return updateInfo{}, err
	}
	info := updateInfo{Current: desktop.Version, Latest: release.Version, Available: newer, Page: release.Page}
	if blocked := a.installBlocked(); blocked != nil {
		info.Blocked = blocked.Error()
	} else {
		info.CanInstall = newer
	}
	a.updateMu.Lock()
	a.release = nil
	if newer {
		a.release = &release
	}
	a.updateMu.Unlock()
	return info, nil
}

// installBlocked keeps updates to the installed app: an isolated development
// run or a copy outside the installation folder would otherwise replace the
// personal installation.
func (a *App) installBlocked() error {
	if a.development {
		return errors.New("Atualização automática indisponível nesta execução de teste.")
	}
	if !equalPath(filepath.Dir(a.executable), filepath.Dir(a.stateRoot)) {
		return errors.New("Este Dwnl não foi aberto da pasta de instalação. Atualize pelo instalador.")
	}
	return nil
}

// InstallUpdate downloads the installer of the version CheckForUpdate found,
// verifies it, opens it and quits, so the installer can replace this app.
func (a *App) InstallUpdate(version string) error {
	if err := a.installBlocked(); err != nil {
		return err
	}
	a.updateMu.Lock()
	release := a.release
	if a.installing || release == nil || release.Version != version {
		a.updateMu.Unlock()
		if a.installing {
			return errors.New("A atualização já está sendo baixada.")
		}
		return errors.New("Busque atualizações de novo antes de instalar.")
	}
	a.installing = true
	a.updateMu.Unlock()
	defer func() {
		a.updateMu.Lock()
		a.installing = false
		a.updateMu.Unlock()
	}()
	dir := filepath.Join(a.stateRoot, "updates")
	if err := hostwin.CreateDirectories(dir, true); err != nil {
		return err
	}
	removeOldInstallers(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	setup, err := a.updates.Fetch(ctx, *release, dir, userAgent())
	if err != nil {
		return err
	}
	if err := shellOpen(setup, ""); err != nil {
		return errors.New("O instalador foi baixado, mas não abriu. Execute " + setup + ".")
	}
	// The installer waits for this app to close before replacing it, and
	// reopens it when it finishes. The queue is saved as with Sair.
	a.quit()
	return nil
}

// OpenUpdatePage opens the notes of the version CheckForUpdate found.
func (a *App) OpenUpdatePage() error {
	a.updateMu.Lock()
	release := a.release
	a.updateMu.Unlock()
	if release == nil {
		return errors.New("Busque atualizações antes de abrir as novidades.")
	}
	return shellOpen(release.Page, "")
}

// removeOldInstallers keeps the updates folder to the download in progress.
func removeOldInstallers(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasPrefix(name, "Dwnl-Setup-") && (strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".part")) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}
