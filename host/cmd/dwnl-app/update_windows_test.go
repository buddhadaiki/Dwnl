package main

import (
	"dwnl/host/internal/desktop"
	"dwnl/host/internal/update"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseServer publishes one release in the shape GitHub's API returns.
func releaseServer(t *testing.T, version string) update.Source {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base, tag := server.URL, "v"+version
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag, "html_url": base + "/pages/" + tag,
			"assets": []map[string]any{
				{"name": "Dwnl-Setup-" + version + "-windows-x64.exe", "browser_download_url": base + "/download/" + tag + "/Dwnl-Setup-" + version + "-windows-x64.exe", "size": 4},
				{"name": "SHA256SUMS.txt", "browser_download_url": base + "/download/" + tag + "/SHA256SUMS.txt", "size": 70},
			},
		})
	}))
	t.Cleanup(server.Close)
	host := server.Listener.Addr().String()
	return update.Source{API: server.URL + "/api", Pages: server.URL + "/pages", Downloads: server.URL + "/download",
		Allowed: func(u *url.URL) bool { return u.Scheme == "https" && u.Host == host }, Transport: server.Client().Transport}
}

func installedApp(t *testing.T) *App {
	t.Helper()
	install := t.TempDir()
	return &App{executable: filepath.Join(install, "Dwnl.exe"), stateRoot: filepath.Join(install, "state")}
}

func TestUpdateCheckReportsANewerReleaseAndOffersToInstallItOnlyWhenInstalled(t *testing.T) {
	app := installedApp(t)
	app.updates = releaseServer(t, "99.0.0")
	info, err := app.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || !info.CanInstall || info.Latest != "99.0.0" || info.Current != desktop.Version || info.Blocked != "" || !strings.HasSuffix(info.Page, "/pages/v99.0.0") {
		t.Fatalf("installed app: %+v", info)
	}

	app.development = true
	if info, err = app.CheckForUpdate(); err != nil || !info.Available || info.CanInstall || info.Blocked == "" {
		t.Fatalf("development run: %+v, %v", info, err)
	}
	if err := app.InstallUpdate("99.0.0"); err == nil || !strings.Contains(err.Error(), "execução de teste") {
		t.Fatalf("a development run installed an update: %v", err)
	}

	copied := &App{executable: filepath.Join(t.TempDir(), "Dwnl.exe"), stateRoot: app.stateRoot, updates: app.updates}
	if info, err = copied.CheckForUpdate(); err != nil || info.CanInstall || !strings.Contains(info.Blocked, "pasta de instalação") {
		t.Fatalf("copy outside the installation: %+v, %v", info, err)
	}
}

func TestUpdateCheckOffersNothingWhenTheReleaseIsNotNewer(t *testing.T) {
	app := installedApp(t)
	app.updates = releaseServer(t, desktop.Version)
	info, err := app.CheckForUpdate()
	if err != nil || info.Available || info.CanInstall || info.Latest != desktop.Version {
		t.Fatalf("same version: %+v, %v", info, err)
	}
	if err := app.InstallUpdate(desktop.Version); err == nil || !strings.Contains(err.Error(), "Busque atualizações") {
		t.Fatalf("installed a version that is not newer: %v", err)
	}
	if err := app.OpenUpdatePage(); err == nil {
		t.Fatal("opened notes of a version that is not newer")
	}
}

func TestUpdateInstallNeedsTheVersionTheCheckFound(t *testing.T) {
	app := installedApp(t)
	app.updates = releaseServer(t, "99.0.0")
	if err := app.InstallUpdate("99.0.0"); err == nil || !strings.Contains(err.Error(), "Busque atualizações") {
		t.Fatalf("installed without a check: %v", err)
	}
	if _, err := app.CheckForUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := app.InstallUpdate("98.0.0"); err == nil || !strings.Contains(err.Error(), "Busque atualizações") {
		t.Fatalf("installed another version than the one found: %v", err)
	}
}

func TestUpdateFolderKeepsOnlyTheDownloadInProgress(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Dwnl-Setup-0.3.1-windows-x64.exe", "Dwnl-Setup-123.part", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "Dwnl-Setup-folder.exe"), 0700); err != nil {
		t.Fatal(err)
	}
	removeOldInstallers(dir)
	entries, _ := os.ReadDir(dir)
	var left []string
	for _, entry := range entries {
		left = append(left, entry.Name())
	}
	if strings.Join(left, ",") != "Dwnl-Setup-folder.exe,notes.txt" {
		t.Fatalf("left %v", left)
	}
}
