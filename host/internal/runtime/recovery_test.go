package runtime

import (
	"context"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/storage"
	"dwnl/host/internal/testutil"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type journalEngine struct {
	blockingEngine
	store *storage.Store
	done  chan error
}

func (e *journalEngine) Execute(_ context.Context, p contracts.DownloadPlan, progress func(contracts.Progress)) (result contracts.CompletedFile, err error) {
	defer func() { e.done <- err }()
	if p.PrepareFinalization == nil {
		return result, errors.New("host plan has no durable preparation callback")
	}
	progress(contracts.Progress{Stage: contracts.StateSaving})
	f, owned, err := storage.CreateIntermediate(p.DestinationPath, p.JobID)
	if err != nil {
		return result, err
	}
	data := []byte("verified runtime media")
	if _, err = f.Write(data); err != nil {
		f.Close()
		return result, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return result, err
	}
	if err = f.Close(); err != nil {
		return result, err
	}
	sum := sha256.Sum256(data)
	result = contracts.CompletedFile{Path: filepath.Join(p.DestinationPath, "published.mp3"), Format: "mp3", SizeBytes: uint64(len(data)), SHA256: hex.EncodeToString(sum[:]), AudioCodec: "mp3", MP3BitrateKbps: 192}
	proof := contracts.PreparedFinalization{File: result, Identity: contracts.PhysicalIdentity(owned.Identity), ParentIdentity: contracts.PhysicalIdentity(owned.ParentIdentity)}
	if err = p.PrepareFinalization(proof); err != nil {
		owned.Cleanup()
		return result, err
	}
	record, err := e.store.Find(p.HostSessionID, p.JobID, "")
	if err != nil || record == nil || record.PreparedFinalization == nil || *record.PreparedFinalization != proof {
		owned.Cleanup()
		return result, errors.New("journal preparation was not durable before publication")
	}
	if err = owned.Publish(result.Path); err != nil {
		owned.Cleanup()
		return result, err
	}
	progress(contracts.Progress{Stage: contracts.StateSaving})
	return result, nil
}

func TestStartSuppliesDurablePreparationAndLaterWritesPreserveIt(t *testing.T) {
	e := &journalEngine{done: make(chan error, 1)}
	s := newTestSession(t, e, filepath.Join(testutil.TempDir(t), "state"))
	e.store = s.options.Store
	response := s.Handle(context.Background(), analyzeStart(t, s, "prepare-start"))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	start, _ := contracts.ResultAs[contracts.StartResult](response)
	select {
	case err := <-e.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned publication did not finish")
	}
	waitState(t, s, start.JobID, contracts.StateCompleted)
	record, err := s.options.Store.Find(s.ID(), start.JobID, "")
	if err != nil || record == nil || record.PreparedFinalization == nil || !record.TerminalConfirmed || record.Snapshot.CompletedFile == nil || record.PreparedFinalization.File != *record.Snapshot.CompletedFile {
		t.Fatalf("terminal journal lost preparation: %+v, %v", record, err)
	}
}

func TestReconcilePreparedCrashWindowAndPhysicalReplacement(t *testing.T) {
	for _, scenario := range []string{"crash", "replacement nonterminal", "replacement terminal", "absent canceled", "tampered failed"} {
		t.Run(scenario, func(t *testing.T) {
			base := testutil.TempDir(t)
			s := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, filepath.Join(base, "state"))
			destination := filepath.Join(base, "approved")
			if err := os.Mkdir(destination, 0700); err != nil {
				t.Fatal(err)
			}
			f, owned, err := storage.CreateIntermediate(destination, "previous-job")
			if err != nil {
				t.Fatal(err)
			}
			data := []byte("prepared media")
			if _, err = f.Write(data); err != nil {
				f.Close()
				t.Fatal(err)
			}
			if err = f.Sync(); err != nil {
				f.Close()
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			file := contracts.CompletedFile{Path: filepath.Join(destination, "final.mp4"), Format: "mp4", SizeBytes: uint64(len(data)), SHA256: hex.EncodeToString(sum[:]), VideoCodec: "h264", AudioCodec: "aac"}
			proof := contracts.PreparedFinalization{File: file, Identity: contracts.PhysicalIdentity(owned.Identity), ParentIdentity: contracts.PhysicalIdentity(owned.ParentIdentity)}
			r := storage.Record{Fingerprint: "old-start", Snapshot: contracts.JobSnapshot{HostSessionID: "previous-session", JobID: "previous-job", StartRequestID: "previous-start", Sequence: 7, State: contracts.StateSaving}}
			if err = s.options.Store.Put(r); err != nil {
				t.Fatal(err)
			}
			if err = s.options.Store.PrepareFinalization("previous-session", "previous-job", proof); err != nil {
				t.Fatal(err)
			}
			if err = owned.Publish(file.Path); err != nil {
				t.Fatal(err)
			}
			wantState, wantOutcome := contracts.StateCompleted, "confirmed"
			switch scenario {
			case "replacement nonterminal", "replacement terminal":
				if scenario == "replacement terminal" {
					r.Snapshot.State, r.TerminalConfirmed, r.Snapshot.CompletedFile = contracts.StateCompleted, true, &file
					if err = s.options.Store.Put(r); err != nil {
						t.Fatal(err)
					}
				}
				if err = os.Rename(file.Path, file.Path+".original"); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(file.Path, data, 0600); err != nil {
					t.Fatal(err)
				}
				wantState, wantOutcome = contracts.StateInterrupted, "unconfirmed"
			case "absent canceled":
				r.Snapshot.State, r.TerminalConfirmed = contracts.StateCanceled, true
				if err = s.options.Store.Put(r); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(file.Path); err != nil {
					t.Fatal(err)
				}
				wantState = contracts.StateCanceled
			case "tampered failed":
				r.Snapshot.State, r.TerminalConfirmed = contracts.StateFailed, true
				r.Snapshot.Error = &contracts.ProtocolError{Code: contracts.InvalidOutput, Message: "media verification failed"}
				if err = s.options.Store.Put(r); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(file.Path, []byte("tampered media"), 0600); err != nil {
					t.Fatal(err)
				}
				wantState = contracts.StateFailed
			}
			before, err := os.ReadFile(filepath.Join(s.options.Store.Root(), "history.json"))
			if err != nil {
				t.Fatal(err)
			}
			response := s.Handle(context.Background(), request("prepared-reconcile", s.ID(), contracts.OperationReconcile, contracts.ReconcilePayload{PreviousHostSessionID: "previous-session", JobID: "previous-job"}))
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			got, _ := contracts.ResultAs[contracts.ReconcileResult](response)
			if got.Outcome != wantOutcome || got.Snapshot == nil || got.Snapshot.State != wantState {
				t.Fatalf("%s reconciliation: %+v", scenario, got)
			}
			if wantState == contracts.StateCompleted && (got.Snapshot.CompletedFile == nil || *got.Snapshot.CompletedFile != file) {
				t.Fatal("prepared summary was not recovered")
			}
			if wantState != contracts.StateCompleted && got.Snapshot.CompletedFile != nil {
				t.Fatal("unconfirmed prepared file leaked as completed")
			}
			after, err := os.ReadFile(filepath.Join(s.options.Store.Root(), "history.json"))
			if err != nil || string(before) != string(after) {
				t.Fatalf("reconcile changed history: %v", err)
			}
			select {
			case <-s.options.Engine.(*blockingEngine).begun:
				t.Fatal("reconcile restarted engine")
			default:
			}
		})
	}
}

func TestChooseDestinationPersistsApprovedPathAndFreshSessionID(t *testing.T) {
	base := testutil.TempDir(t)
	chosen, fallback := filepath.Join(base, "chosen"), filepath.Join(base, "downloads")
	if err := os.Mkdir(chosen, 0700); err != nil {
		t.Fatal(err)
	}
	s := newTestSession(t, &blockingEngine{begun: make(chan struct{})}, filepath.Join(base, "state"))
	pickerCanceled := false
	dest, err := storage.NewDestinations(fallback, func(context.Context) (string, bool, error) { return chosen, pickerCanceled, nil })
	if err != nil {
		t.Fatal(err)
	}
	s.options.Destinations = dest
	response := s.Handle(context.Background(), request("choose-a", s.ID(), contracts.OperationChooseDestination, contracts.ChooseDestinationPayload{UserGesture: true}))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	selected, _ := contracts.ResultAs[contracts.ChooseDestinationResult](response)
	path, err := s.options.Store.PreferredDestination(fallback)
	if err != nil || path != chosen {
		t.Fatalf("chosen folder was not persisted: %q, %v", path, err)
	}
	before, err := os.ReadFile(filepath.Join(s.options.Store.Root(), "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	pickerCanceled = true
	response = s.Handle(context.Background(), request("choose-cancel", s.ID(), contracts.OperationChooseDestination, contracts.ChooseDestinationPayload{UserGesture: true}))
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	after, err := os.ReadFile(filepath.Join(s.options.Store.Root(), "preferences.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("canceling picker changed preference")
	}
	reopened, err := storage.Open(s.options.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	path, err = reopened.PreferredDestination(fallback)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := storage.NewDestinations(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Default().Label != chosen || fresh.Default().DestinationID == selected.DestinationID {
		t.Fatal("new session did not regenerate destination authority")
	}
	if _, err = fresh.Resolve(selected.DestinationID); err == nil {
		t.Fatal("old opaque ID remained valid")
	}
}
