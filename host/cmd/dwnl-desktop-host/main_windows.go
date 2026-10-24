// dwnl-desktop-host is a thin Chrome Native Messaging bridge. EOF closes only
// this bridge; the desktop application owns all downloads and their lifetime.
package main

import (
	"context"
	"dwnl/host/internal/desktopipc"
	"dwnl/host/internal/native"
	hostwin "dwnl/host/internal/windows"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const origin = "chrome-extension://lphdonkmiecidfgjfplhdfnpphmdioeh/"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Dwnl: a ponte não pôde conectar ao aplicativo:", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 || os.Args[1] != origin {
		return errors.New("origem não autorizada")
	}
	state, _, err := hostwin.DefaultPaths()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	app := filepath.Join(filepath.Dir(executable), "Dwnl.exe")
	if err = hostwin.CheckPath(app, false); err != nil {
		return err
	}
	return serveBridge(state, app, os.Stdin, os.Stdout, launchDesktop)
}

// The child outlives Native Messaging. It is intentionally not CommandContext:
// closing Chrome must never terminate the application's accepted queue.
func launchDesktop(app string) error {
	cmd := exec.Command(app, "--background")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000200 | 0x00000008}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

type appConnector struct {
	state, app string
	launch     func(string) error
	start      sync.Mutex
}

// A connected pipe proves the app is running; slow replies are the request's
// own timeout, never a reason to launch a second instance.
func (c *appConnector) ping(ctx context.Context) error {
	probe, stop := context.WithTimeout(ctx, 350*time.Millisecond)
	defer stop()
	return desktopipc.Dial(probe, c.state)
}
func (c *appConnector) ready(ctx context.Context) error {
	if err := c.ping(ctx); err == nil {
		return nil
	}
	// Serialise each launch attempt, not the bridge lifetime. A later crash
	// must allow another launch; concurrent callers recheck after acquiring.
	c.start.Lock()
	defer c.start.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.ping(ctx); err == nil {
		return nil
	}
	if err := c.launch(c.app); err != nil {
		return err
	}
	wait, stop := context.WithTimeout(ctx, 12*time.Second)
	defer stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := c.ping(wait); err == nil {
			return nil
		}
		select {
		case <-wait.Done():
			return wait.Err()
		case <-ticker.C:
		}
	}
}

// Production validates the Chrome origin and owned executable before entering.
// An injected launch and isolated state make the complete framing/pipe boundary
// testable without installing a host or changing the user's registry/profile.
func serveBridge(state, app string, input io.ReadCloser, output io.Writer, launch func(string) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var write sync.Mutex
	var failureMu sync.Mutex
	var outputFailure error
	// An output failure can happen while the read loop awaits Chrome's next
	// frame. Closing the owned input unblocks it and guarantees worker cleanup.
	closeIO := func() {
		_ = input.Close()
		if closer, ok := output.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	stopIO := context.AfterFunc(ctx, closeIO)
	defer func() { cancel(); closeIO(); wg.Wait(); stopIO() }()
	failOutput := func(err error) {
		failureMu.Lock()
		if outputFailure == nil {
			outputFailure = err
		}
		failureMu.Unlock()
		cancel()
	}
	connection := appConnector{state: state, app: app, launch: launch}
	slots := make(chan struct{}, 8)
	for {
		frame, err := native.ReadFrame(input)
		if err != nil {
			failureMu.Lock()
			failure := outputFailure
			failureMu.Unlock()
			if failure != nil {
				return failure
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		req, err := desktopipc.Decode(frame)
		if err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			timeout := 40 * time.Second
			if req.Method == "analyze" {
				timeout = 125 * time.Second
			}
			if req.Method == "chooseDestination" {
				timeout = 305 * time.Second
			}
			work, done := context.WithTimeout(ctx, timeout)
			defer done()
			connectErr := connection.ready(work)
			var response desktopipc.Response
			if connectErr == nil {
				// Only establishment is retried. Once sent, a mutation may have
				// been accepted even if its reply was lost: never replay here.
				response, connectErr = desktopipc.Call(work, state, req)
			}
			if connectErr != nil {
				response = desktopipc.Response{Version: 2, ID: req.ID, Error: &desktopipc.Error{Code: "APP_UNAVAILABLE", Message: "O aplicativo não respondeu. Abra o Dwnl e tente reconectar; verifique a fila antes de repetir um download."}}
			}
			data, err := json.Marshal(response)
			if err != nil {
				failOutput(err)
				return
			}
			write.Lock()
			defer write.Unlock()
			if ctx.Err() == nil {
				if err = native.WriteFrame(output, data); err != nil {
					failOutput(err)
				}
			}
		}()
	}
}
