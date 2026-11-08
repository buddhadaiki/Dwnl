package main

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/desktopipc"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Exit codes of "Dwnl.exe --quit", which the installer reads before it
// replaces the installation.
const (
	quitDone        = 0 // no Dwnl was running, or it has closed
	quitFailed      = 1 // it could not be asked, or did not close in time
	quitBusy        = 3 // a download is in progress; --force closes it anyway
	quitUnsupported = 4 // the running Dwnl predates --quit
)

// instanceScope names the lease that keeps one Dwnl per state folder.
func instanceScope(address string) string {
	hash := sha256.Sum256([]byte(address))
	return "desktop-" + hex.EncodeToString(hash[:8])
}

// quitScope names the signal a running Dwnl waits on to quit. It is a kernel
// event, not a pipe method, so the extension bridge cannot reach it.
func quitScope(instance string) string { return "quit-" + instance }

// closeRunning asks the Dwnl that owns stateRoot to quit, as Sair does, and
// waits up to wait for it to exit.
func closeRunning(stateRoot, instance string, force bool, wait time.Duration) int {
	lease, busy, err := hostwin.AcquireLease(instance, false)
	if err != nil {
		return quitFailed
	}
	if !busy {
		lease.Close()
		return quitDone
	}
	if !force {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		active, err := downloading(ctx, stateRoot)
		cancel()
		if err == nil && active {
			return quitBusy
		}
	}
	raised, err := hostwin.RaiseSignal(quitScope(instance))
	if err != nil {
		return quitFailed
	}
	if !raised {
		return quitUnsupported
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
		lease, busy, err := hostwin.AcquireLease(instance, false)
		if err == nil && !busy {
			lease.Close()
			return quitDone
		}
	}
	return quitFailed
}

// downloading reports whether the running Dwnl is working on a download.
func downloading(ctx context.Context, stateRoot string) (bool, error) {
	response, err := desktopipc.Call(ctx, stateRoot, desktopipc.Request{Version: 2, ID: secondInstanceID, Method: "snapshot", Payload: json.RawMessage(`{}`)})
	if err != nil {
		return false, err
	}
	if response.Error != nil {
		return false, errors.New(response.Error.Message)
	}
	return activeTask(response.Result)
}

// activeTask reads a snapshot for a task between queued and finished.
func activeTask(result any) (bool, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return false, err
	}
	var snapshot struct {
		Tasks []struct {
			State string `json:"state"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return false, err
	}
	for _, task := range snapshot.Tasks {
		switch task.State {
		case "queued", "completed", "failed", "canceled", "interrupted":
		default:
			return true, nil
		}
	}
	return false, nil
}
