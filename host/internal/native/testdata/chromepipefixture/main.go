// Workspace-only instrumentation for Chrome's native-host pipe contract.
// It never uses installed state, the registry, a browser profile or media tools.
package main

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/native"
	hostruntime "dwnl/host/internal/runtime"
	"dwnl/host/internal/storage"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type unusedEngine struct{}

func (unusedEngine) Analyze(context.Context, string) (contracts.Analysis, error) {
	panic("pipe fixture cannot analyze media")
}
func (unusedEngine) Execute(context.Context, contracts.DownloadPlan, func(contracts.Progress)) (contracts.CompletedFile, error) {
	panic("pipe fixture cannot download media")
}
func main() {
	fmt.Fprintf(os.Stderr, "stage=main args=%q\n", os.Args[1:])
	const extension = "lphdonkmiecidfgjfplhdfnpphmdioeh"
	if len(os.Args) < 2 || os.Args[1] != "chrome-extension://"+extension+"/" {
		panic("unexpected origin")
	}
	mode := os.Getenv("DWNL_PIPE_FIXTURE_MODE")
	if mode == "launcher" {
		if err := launchFixture(); err != nil {
			fmt.Fprintln(os.Stderr, "stage=launcher-return", err)
			os.Exit(1)
		}
		return
	}
	if mode == "stall-write" {
		stall()
	}
	if mode == "stall-read" || mode == "stall-exit" {
		body, err := native.ReadFrame(os.Stdin)
		if err != nil {
			panic(err)
		}
		if mode == "stall-exit" {
			if err := native.WriteFrame(os.Stdout, body); err != nil {
				panic(err)
			}
		}
		stall()
	}
	if os.Getenv("DWNL_PIPE_FIXTURE_MODE") == "raw" {
		for {
			body, err := native.ReadFrame(os.Stdin)
			fmt.Fprintf(os.Stderr, "stage=read bytes=%d error=%v\n", len(body), err)
			if err == io.EOF {
				return
			}
			if err != nil {
				panic(err)
			}
			if err := native.WriteFrame(os.Stdout, body); err != nil {
				panic(err)
			}
		}
	}
	root := os.Getenv("DWNL_PIPE_FIXTURE_ROOT")
	store, err := storage.Open(filepath.Join(root, "state"))
	if err != nil {
		panic(err)
	}
	dest, err := storage.NewDestinations(filepath.Join(root, "downloads"), nil)
	if err != nil {
		panic(err)
	}
	session, err := hostruntime.NewSession(hostruntime.Options{
		SessionID: "pipe-session", Engine: unusedEngine{}, Store: store, Destinations: dest,
		Origin: os.Args[1], ExtensionID: extension,
		Handshake: contracts.HandshakeResult{HostVersion: "pipe-fixture", EngineVersion: "unused", Dependencies: []contracts.Dependency{}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, "stage=serve")
	err = native.Serve(context.Background(), os.Stdin, os.Stdout, session)
	fmt.Fprintf(os.Stderr, "stage=serve-return error=%v\n", err)
	if err != nil {
		os.Exit(1)
	}
}

func stall() {
	fmt.Fprintf(os.Stderr, "stage=stall pid=%d\n", os.Getpid())
	for {
		time.Sleep(time.Hour)
	}
}
