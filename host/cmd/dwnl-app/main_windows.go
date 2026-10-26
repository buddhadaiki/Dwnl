package main

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/desktop"
	"dwnl/host/internal/desktopipc"
	hostwin "dwnl/host/internal/windows"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	winoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows/registry"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed frontend/index.html frontend/style.css frontend/app.js frontend/model.mjs
var assets embed.FS

//go:embed icon.png
var icon []byte

type App struct {
	service     *desktop.Service
	ctx         context.Context
	ready       chan struct{}
	stop        chan struct{}
	quitOnce    sync.Once
	executable  string
	development bool
	quitting    atomic.Bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Dwnl:", err)
		showStartupError(err.Error())
		os.Exit(1)
	}
}
func run() error {
	started := time.Now()
	defaultState, defaultDestination, err := hostwin.DefaultPaths()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	state := flag.String("state-dir", defaultState, "Estado isolado para desenvolvimento")
	bin := flag.String("bin-dir", filepath.Join(filepath.Dir(exe), "bin"), "Executáveis privados")
	destination := flag.String("destination", defaultDestination, "Destino inicial")
	background := flag.Bool("background", false, "Iniciar na bandeja")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("argumentos desconhecidos")
	}
	for _, p := range []string{*state, *bin, *destination} {
		if !filepath.IsAbs(p) {
			return errors.New("caminhos absolutos são obrigatórios")
		}
	}
	address, err := desktopipc.Address(*state)
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(address))
	lease, busy, err := hostwin.AcquireLease("desktop-"+hex.EncodeToString(hash[:8]), false)
	if err != nil {
		return err
	}
	if busy {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		for {
			method := "openApp"
			if *background {
				method = "snapshot"
			}
			response, e := desktopipc.Call(ctx, *state, desktopipc.Request{Version: 2, ID: "second-instance", Method: method, Payload: json.RawMessage(`{}`)})
			if e == nil && response.Error == nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("O Dwnl já está aberto, mas não respondeu. Verifique sua janela ou bandeja.")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	defer lease.Close()
	service, err := desktop.New(desktop.Config{StateRoot: *state, BinDir: *bin, DefaultDestination: *destination})
	if err != nil {
		return err
	}
	defer service.Close()
	app := &App{service: service, ready: make(chan struct{}), stop: make(chan struct{}), executable: exe, development: !equalPath(*state, defaultState)}
	server, err := desktopipc.Listen(*state, app.dispatch)
	if err != nil {
		return err
	}
	defer server.Close()
	frontend, err := fs.Sub(assets, "frontend")
	if err != nil {
		return err
	}
	// Paint the theme's page colour before the frontend loads, so opening the
	// window never flashes the other theme.
	backgroundColour := options.NewRGB(28, 28, 28)
	if appsUseLightTheme() {
		backgroundColour = options.NewRGB(243, 243, 243)
	}
	return wails.Run(&options.App{Title: "Dwnl", Width: 1120, Height: 780, MinWidth: 760, MinHeight: 560, StartHidden: *background, HideWindowOnClose: true, BackgroundColour: backgroundColour, AssetServer: &assetserver.Options{Assets: frontend}, Bind: []interface{}{app},
		OnStartup: func(ctx context.Context) { app.ctx = ctx; close(app.ready); go app.tray() },
		OnDomReady: func(context.Context) {
			// Isolated development runs expose cold-start timing without telemetry.
			if app.development {
				fmt.Fprintf(os.Stderr, "Dwnl DOM ready: %dms\n", time.Since(started).Milliseconds())
			}
		},
		OnBeforeClose: func(ctx context.Context) bool {
			if app.quitting.Load() {
				return false
			}
			wailsruntime.WindowHide(ctx)
			return true
		},
		OnShutdown: func(context.Context) { app.quitOnce.Do(func() { close(app.stop); systray.Quit() }) },
		Windows:    &winoptions.Options{Theme: winoptions.SystemDefault, BackdropType: winoptions.None, WebviewUserDataPath: filepath.Join(*state, "webview"), WindowClassName: "DwnlDesktopWindow", DLLSearchPaths: winoptions.DLLSearchSystem32 | winoptions.DLLSearchApplicationDir}})
}
func (a *App) Call(method string, payload string) (any, error) {
	if len(payload) > 524288 {
		return nil, errors.New("Pedido muito grande")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 125*time.Second)
	if method == "chooseDestination" {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	}
	defer cancel()
	return a.dispatch(ctx, method, json.RawMessage(payload))
}
func (a *App) show(ctx context.Context) error {
	select {
	case <-a.ready:
		wailsruntime.WindowShow(a.ctx)
		wailsruntime.WindowUnminimise(a.ctx)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Exit is exposed only to the local window, never to the extension IPC.
func (a *App) Exit() { a.quit() }

func (a *App) quit() {
	a.quitting.Store(true)
	select {
	case <-a.ready:
		wailsruntime.Quit(a.ctx)
	default:
	}
}
func (a *App) tray() {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	systray.Run(func() {
		systray.SetIcon(pngICO(icon))
		systray.SetTooltip("Dwnl — seus downloads continuam aqui")
		open := systray.AddMenuItem("Abrir Dwnl", "Abrir fila e histórico")
		systray.AddSeparator()
		exit := systray.AddMenuItem("Sair", "Interromper o trabalho e preservar a fila")
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					_ = a.show(context.Background())
				case <-exit.ClickedCh:
					a.quit()
					return
				case <-a.stop:
					return
				}
			}
		}()
	}, func() {})
}
func pngICO(png []byte) []byte {
	result := make([]byte, 22+len(png))
	binary.LittleEndian.PutUint16(result[2:4], 1)
	binary.LittleEndian.PutUint16(result[4:6], 1)
	result[6] = 128
	result[7] = 128
	binary.LittleEndian.PutUint16(result[10:12], 1)
	binary.LittleEndian.PutUint16(result[12:14], 32)
	binary.LittleEndian.PutUint32(result[14:18], uint32(len(png)))
	binary.LittleEndian.PutUint32(result[18:22], 22)
	copy(result[22:], png)
	return result
}

const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

// appsUseLightTheme reads the Windows "app mode" setting; dark is the fallback.
func appsUseLightTheme() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	return err == nil && value != 0
}
