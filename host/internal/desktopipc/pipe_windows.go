package desktopipc

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/native"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"encoding/json"
	"errors"
	winio "github.com/Microsoft/go-winio"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func Address(stateRoot string) (string, error) {
	id, err := hostwin.UserID()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(id + "|" + strings.ToLower(filepath.Clean(stateRoot))))
	return `\\.\pipe\Dwnl.Desktop.` + hex.EncodeToString(sum[:16]), nil
}

type Server struct {
	listener net.Listener
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func Listen(stateRoot string, handler Handler) (*Server, error) {
	address, err := Address(stateRoot)
	if err != nil {
		return nil, err
	}
	id, err := hostwin.UserID()
	if err != nil {
		return nil, err
	}
	listener, err := winio.ListenPipe(address, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + id + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{listener: listener, cancel: cancel}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		slots := make(chan struct{}, 8)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				conn.Close()
				return
			default:
				conn.Close()
				continue
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() { <-slots }()
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				data, err := native.ReadFrame(conn)
				if err != nil {
					return
				}
				req, err := Decode(data)
				if err != nil {
					return
				}
				timeout := 35 * time.Second
				if req.Method == "analyze" {
					timeout = 120 * time.Second
				}
				if req.Method == "chooseDestination" {
					timeout = 5 * time.Minute
				}
				work, done := context.WithTimeout(ctx, timeout)
				defer done()
				_ = conn.SetDeadline(time.Now().Add(timeout + time.Second))
				// One connection carries one request. Observe EOF while the handler
				// runs so a closed browser cancels analysis/pickers promptly.
				// Accepted downloads use the service context and survive this.
				peerDone := make(chan struct{})
				go func() {
					var trailing [1]byte
					_, _ = conn.Read(trailing[:])
					done()
					close(peerDone)
				}()
				defer func() { conn.Close(); <-peerDone }()
				response := Handle(work, handler, req)
				encoded, err := json.Marshal(response)
				if err != nil {
					return
				}
				if len(encoded) > 524288 {
					encoded, _ = json.Marshal(Response{Version: Version, ID: req.ID, Error: &Error{Code: "MESSAGE_TOO_LARGE", Message: "A resposta excedeu o limite. Reduza o histórico."}})
				}
				_ = native.WriteFrame(conn, encoded)
			}()
		}
	}()
	return s, nil
}
func (s *Server) Close() { s.cancel(); s.listener.Close(); s.wg.Wait() }

// Dial reports whether the app is listening without asking it to do any work,
// so a busy app is never mistaken for a missing one.
func Dial(ctx context.Context, stateRoot string) error {
	address, err := Address(stateRoot)
	if err != nil {
		return err
	}
	conn, err := winio.DialPipeContext(ctx, address)
	if err != nil {
		return err
	}
	return conn.Close()
}
func Call(ctx context.Context, stateRoot string, req Request) (Response, error) {
	var response Response
	data, err := json.Marshal(req)
	if err != nil {
		return response, err
	}
	if _, err = Decode(data); err != nil {
		return response, err
	}
	address, err := Address(stateRoot)
	if err != nil {
		return response, err
	}
	conn, err := winio.DialPipeContext(ctx, address)
	if err != nil {
		return response, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err = native.WriteFrame(conn, data); err != nil {
		return response, err
	}
	data, err = native.ReadFrame(conn)
	if err != nil {
		return response, err
	}
	if err = json.Unmarshal(data, &response); err != nil {
		return response, err
	}
	if response.Version != Version || response.ID != req.ID {
		return response, errors.New("Resposta incompatível do aplicativo")
	}
	return response, nil
}
