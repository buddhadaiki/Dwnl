package native

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"dwnl/host/internal/contracts"
	win "golang.org/x/sys/windows"
)

// Reproduce the actual CfT149 Win32 pipe modes and minimal client rights from:
// chromium/chromium/149.0.7827.55/.../messaging/launch_context_win.cc:86-123,199-213.
// This tests binary framing/Serve in a fresh Go process without installed state.
func TestChromeNamedPipeStdio(t *testing.T) {
	fixture := buildChromeFixture(t)
	for _, tc := range []struct{ mode, launcher string }{{"raw", "direct"}, {"serve", "direct"}, {"raw", "cmd"}, {"serve", "cmd"}} {
		t.Run(tc.launcher+"/"+tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			input, output := chromePipe(t, ctx, true), chromePipe(t, ctx, false)
			process := startChromeFixture(t, ctx, cancel, fixture, tc.launcher, tc.mode, input, output)
			if err := input.connect(); err != nil {
				t.Fatal(err)
			}
			if err := output.connect(); err != nil {
				t.Fatal(err)
			}
			request := []byte(`{"protocolVersion":1,"requestId":"chrome-pipe-handshake","operation":"handshake","payload":{"extensionVersion":"0.1.0"}}`)
			writeErr := WriteFrame(input, request)
			var reply []byte
			var readErr error
			if writeErr == nil {
				reply, readErr = ReadFrame(output)
			}
			input.Close()
			if !process.wait(12 * time.Second) {
				t.Fatal("owned fixture did not exit/drain before the wait deadline")
			}
			t.Logf("launcher=%s mode=%s write=%v read=%v result=%+v wait=%v stderr=%s", tc.launcher, tc.mode, writeErr, readErr, process.result, process.err, process.stderr())
			if writeErr != nil || readErr != nil || process.err != nil {
				t.Fatalf("Chrome pipe boundary failed: write=%v read=%v wait=%v", writeErr, readErr, process.err)
			}
			if tc.mode == "raw" {
				if !bytes.Equal(reply, request) {
					t.Fatalf("framing changed bytes: %q", reply)
				}
			} else {
				message, err := contracts.DecodeMessage(reply)
				if err != nil || message.Response == nil || message.Response.Error != nil {
					t.Fatalf("invalid Serve handshake: %s (%v)", reply, err)
				}
				var response map[string]any
				if json.Unmarshal(reply, &response) != nil || response["requestId"] != "chrome-pipe-handshake" || response["hostSessionId"] != "pipe-session" {
					t.Fatalf("wrong response: %s", reply)
				}
			}
		})
	}
}

// Each stalled child retains native pipes and inherited stderr. Cancellation
// must finish both I/O and the complete CMD/fixture tree within a finite budget.
func TestChromeNamedPipeCancellation(t *testing.T) {
	fixture := buildChromeFixture(t)
	for _, mode := range []string{"stall-read", "stall-write", "stall-exit"} {
		t.Run("cmd/"+mode, func(t *testing.T) {
			started := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()
			input, output := chromePipe(t, ctx, true), chromePipe(t, ctx, false)
			process := startChromeFixture(t, ctx, cancel, fixture, "cmd", mode, input, output)
			if err := input.connect(); err != nil {
				t.Fatal(err)
			}
			if err := output.connect(); err != nil {
				t.Fatal(err)
			}
			var ioErr error
			if mode == "stall-write" {
				// Exceed the pipe buffer while the fixture never reads its stdin.
				ioErr = WriteFrame(input, bytes.Repeat([]byte("x"), 256*1024))
			} else {
				if err := WriteFrame(input, []byte(`{"fixture":"cancellation"}`)); err != nil {
					t.Fatal(err)
				}
				_, ioErr = ReadFrame(output)
				if mode == "stall-exit" && ioErr != nil {
					t.Fatalf("fixture did not reply before retaining pipes: %v", ioErr)
				}
			}
			if mode != "stall-exit" && !errors.Is(ioErr, context.DeadlineExceeded) {
				t.Errorf("stalled %s I/O did not report its deadline: %v", mode, ioErr)
			}
			input.Close()
			if !process.wait(12 * time.Second) {
				t.Fatal("owned stalled fixture did not finish before cancellation/drain deadline")
			}
			exited, err := process.childExited()
			if !process.result.Canceled || !errors.Is(process.err, context.DeadlineExceeded) || !exited || err != nil {
				t.Fatalf("fixture tree exit/drain unconfirmed: result=%+v err=%v physicalExit=%t identityErr=%v stderr=%s", process.result, process.err, exited, err, process.stderr())
			}
			if pending := input.pending.Load() + output.pending.Load(); pending != 0 {
				t.Fatalf("overlapped operations not drained: %d", pending)
			}
			if elapsed := time.Since(started); elapsed > 8*time.Second {
				t.Fatalf("fixture cancellation exceeded total budget: %v", elapsed)
			}
			t.Logf("mode=%s elapsed=%v runnerCanceled=%t exactFixtureHandleExited=%t pendingIO=0", mode, time.Since(started), process.result.Canceled, exited)
		})
	}
}

func TestChromePipeConnectionDeadlineWithoutClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	pipe := chromePipe(t, ctx, true)
	started := time.Now()
	if err := pipe.connect(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unconnected pipe did not report deadline: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second || pipe.pending.Load() != 0 {
		t.Fatalf("connection cancel/drain was not bounded: elapsed=%v pending=%d", elapsed, pipe.pending.Load())
	}
}

func chromePipe(t *testing.T, ctx context.Context, input bool) *chromeServerPipe {
	t.Helper()
	access := uint32(win.PIPE_ACCESS_INBOUND)
	if input {
		access = win.PIPE_ACCESS_OUTBOUND
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	pipeName := fmt.Sprintf(`\\.\pipe\dwnl.test.chrome.%d.%x`, os.Getpid(), token)
	name, _ := win.UTF16PtrFromString(pipeName)
	server, err := win.CreateNamedPipe(name, access|win.FILE_FLAG_OVERLAPPED|win.FILE_FLAG_FIRST_PIPE_INSTANCE, win.PIPE_TYPE_BYTE, 1, 0, 0, 5000, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &chromeServerPipe{handle: server, name: pipeName, ctx: ctx}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close owned server pipe: %v", err)
		}
	})
	return s
}

// Explicit overlapped I/O matches Chromium's server handles. Every operation
// owns a pinned buffer/OVERLAPPED until the kernel confirms completion.
type chromeServerPipe struct {
	handle  win.Handle
	name    string
	ctx     context.Context
	pending atomic.Int32
}

type chromePipeOperation struct {
	over   win.Overlapped
	buffer []byte
	pins   runtime.Pinner
}

func (p *chromeServerPipe) operation(buf []byte) (*chromePipeOperation, error) {
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	event, err := win.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, err
	}
	op := &chromePipeOperation{over: win.Overlapped{HEvent: event}, buffer: append([]byte(nil), buf...)}
	op.pins.Pin(&op.over)
	if len(op.buffer) != 0 {
		op.pins.Pin(&op.buffer[0])
	}
	return op, nil
}

func (op *chromePipeOperation) close() {
	op.pins.Unpin()
	win.CloseHandle(op.over.HEvent)
}

func (p *chromeServerPipe) connect() error {
	op, err := p.operation(nil)
	if err != nil {
		return err
	}
	err = win.ConnectNamedPipe(p.handle, &op.over)
	_, err = p.complete(op, err)
	if err == win.ERROR_PIPE_CONNECTED {
		return nil
	}
	return err
}

func (p *chromeServerPipe) Read(buf []byte) (int, error)  { return p.transfer(buf, false) }
func (p *chromeServerPipe) Write(buf []byte) (int, error) { return p.transfer(buf, true) }
func (p *chromeServerPipe) Close() error {
	if p.handle == 0 {
		return nil
	}
	err := win.CloseHandle(p.handle)
	p.handle = 0
	return err
}
func (p *chromeServerPipe) transfer(buf []byte, write bool) (int, error) {
	op, err := p.operation(buf)
	if err != nil {
		return 0, err
	}
	var count uint32
	if write {
		err = win.WriteFile(p.handle, op.buffer, &count, &op.over)
	} else {
		err = win.ReadFile(p.handle, op.buffer, &count, &op.over)
	}
	if err == win.ERROR_IO_PENDING {
		count, err = p.complete(op, err)
	} else {
		op.close()
	}
	if !write && count != 0 {
		copy(buf, op.buffer[:count])
	}
	if p.ctx.Err() != nil {
		return 0, p.ctx.Err()
	}
	if err == win.ERROR_BROKEN_PIPE {
		err = io.EOF
	}
	return int(count), err
}

func (p *chromeServerPipe) complete(op *chromePipeOperation, initial error) (uint32, error) {
	if initial != win.ERROR_IO_PENDING {
		op.close()
		return 0, initial
	}
	p.pending.Add(1)
	for p.ctx.Err() == nil {
		status, err := win.WaitForSingleObject(op.over.HEvent, 25)
		if err != nil || status == win.WAIT_OBJECT_0 {
			var count uint32
			if err == nil {
				err = win.GetOverlappedResult(p.handle, &op.over, &count, false)
			}
			if status == win.WAIT_OBJECT_0 {
				p.pending.Add(-1)
				op.close()
				return count, err
			}
			break
		}
	}
	// CancelIoEx only requests cancellation. Do not free the pinned buffer or
	// OVERLAPPED until its event confirms physical completion, even on failure.
	cause := p.ctx.Err()
	if cause == nil {
		cause = errors.New("owned pipe event wait failed")
	}
	cancelErr := win.CancelIoEx(p.handle, &op.over)
	status, drainErr := win.WaitForSingleObject(op.over.HEvent, 2000)
	if drainErr == nil && status == win.WAIT_OBJECT_0 {
		var count uint32
		_ = win.GetOverlappedResult(p.handle, &op.over, &count, false)
		p.pending.Add(-1)
		op.close()
		return 0, cause
	}
	// A broken OS cancellation must fail the test without hanging its caller
	// or freeing memory still referenced by the kernel. Retain this operation
	// asynchronously until completion; pending!=0 prevents a successful test.
	go func() {
		for {
			if state, err := win.WaitForSingleObject(op.over.HEvent, 1000); err == nil && state == win.WAIT_OBJECT_0 {
				p.pending.Add(-1)
				op.close()
				return
			}
		}
	}()
	return 0, fmt.Errorf("owned pipe cancellation/drain unconfirmed: cancel=%v drain=%v status=%d: %w", cancelErr, drainErr, status, cause)
}
