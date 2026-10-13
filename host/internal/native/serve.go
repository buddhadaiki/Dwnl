package native

import (
	"context"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/runtime"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// MaxPendingControlFrames bounds control-memory use; it is not a media job queue.
const MaxPendingControlFrames = 16

var ErrControlOverflow = errors.New("too many pending native control frames")

// Serve owns the connection lifetime. Its reader observes EOF even while a
// request executes, so losing Chrome also cancels an in-flight analysis.
func Serve(parent context.Context, input io.Reader, output io.Writer, session *runtime.Session) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer session.Close()
	var writerMu sync.Mutex
	var errorMu sync.Mutex
	var writeErr error
	write := func(message any) error {
		b, err := json.Marshal(message)
		if err != nil {
			return err
		}
		writerMu.Lock()
		defer writerMu.Unlock()
		return WriteFrame(output, b)
	}
	failWrite := func(err error) {
		errorMu.Lock()
		if writeErr == nil {
			writeErr = err
		}
		errorMu.Unlock()
		cancel()
	}
	if err := session.SetEmitter(func(event contracts.JobEvent) {
		if ctx.Err() != nil {
			return
		}
		if err := write(event); err != nil {
			failWrite(err)
		}
	}); err != nil {
		return err
	}
	requests := make(chan contracts.Request, MaxPendingControlFrames)
	readErr := make(chan error, 1)
	go func() {
		defer close(requests)
		for {
			body, err := ReadFrame(input)
			if err != nil {
				readErr <- err
				cancel()
				return
			}
			request, err := contracts.DecodeRequest(body)
			if err != nil {
				readErr <- err
				cancel()
				return
			}
			select {
			case requests <- request:
			case <-ctx.Done():
				return
			default:
				// Never wait for a busy handler here: EOF behind queued control
				// messages must remain observable. Overflow ends the connection
				// and cancels owned work rather than growing an unbounded queue.
				readErr <- ErrControlOverflow
				cancel()
				return
			}
		}
	}()
	closed := make(chan struct{})
	go func() { <-ctx.Done(); session.Close(); close(closed) }()
	defer func() { cancel(); <-closed }()
	for {
		select {
		case <-ctx.Done():
			errorMu.Lock()
			err := writeErr
			errorMu.Unlock()
			if err != nil {
				return err
			}
			select {
			case err = <-readErr:
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			default:
				return parent.Err()
			}
		case request, ok := <-requests:
			if !ok {
				continue
			}
			response := session.Handle(ctx, request)
			if ctx.Err() != nil {
				continue
			}
			if err := write(response); err != nil {
				failWrite(err)
			}
		}
	}
}
