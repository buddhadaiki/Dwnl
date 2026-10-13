// Package storage persists only user-owned control snapshots, never extraction URLs.
package storage

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"dwnl/host/internal/contracts"
	hostwin "dwnl/host/internal/windows"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

var ErrCorrupt = errors.New("corrupt private ledger")

type Record struct {
	Fingerprint          string                          `json:"fingerprint"`
	TerminalConfirmed    bool                            `json:"terminalConfirmed"`
	Snapshot             contracts.JobSnapshot           `json:"snapshot"`
	PreparedFinalization *contracts.PreparedFinalization `json:"preparedFinalization,omitempty"`
}

func (r *Record) UnmarshalJSON(data []byte) error {
	type recordJSON Record
	var decoded recordJSON
	if strictJSON(data, &decoded) != nil {
		return ErrCorrupt
	}
	fields, err := requiredPrivateFields(data, "fingerprint terminalConfirmed snapshot")
	if err != nil {
		return err
	}
	if _, err = requiredPrivateFields(fields["snapshot"], "hostSessionId jobId startRequestId sequence state"); err != nil {
		return err
	}
	if raw, present := fields["preparedFinalization"]; present {
		proof, err := requiredPrivateFields(raw, "file identity parentIdentity")
		if err != nil || decoded.PreparedFinalization == nil {
			return ErrCorrupt
		}
		for _, field := range []string{"identity", "parentIdentity"} {
			if _, err = requiredPrivateFields(proof[field], "volumeSerialNumber fileIndexHigh fileIndexLow"); err != nil {
				return err
			}
		}
	}
	*r = Record(decoded)
	return nil
}

func requiredPrivateFields(data []byte, required string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, ErrCorrupt
	}
	for _, key := range strings.Fields(required) {
		if raw, present := fields[key]; !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, ErrCorrupt
		}
	}
	return fields, nil
}

type Store struct {
	root  string
	owner string
	mu    sync.Mutex
}
type ledger struct {
	Version int      `json:"version"`
	Owner   string   `json:"owner"`
	Records []Record `json:"records"`
}
type ownerMark struct {
	Version int    `json:"version"`
	Owner   string `json:"owner"`
}

func Open(root string) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = hostwin.CheckPath(root, true); err != nil {
		return nil, err
	}
	owner, err := hostwin.UserID()
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, owner: owner}
	_, err = s.read()
	if err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) Root() string { return s.root }
func (s *Store) locked(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, _, err := hostwin.AcquireLease("ledger:"+strings.ToLower(s.root), true)
	if err != nil {
		return err
	}
	defer lease.Close()
	return fn()
}
func (s *Store) Put(r Record) error {
	return s.locked(func() error {
		if r.Snapshot.Error != nil {
			copy := *r.Snapshot.Error
			copy.Message = "Task ended with " + string(copy.Code)
			r.Snapshot.Error = &copy
		}
		if err := validateRecord(r); err != nil {
			return err
		}
		l, err := s.read()
		if err != nil {
			return err
		}
		updated := false
		for i, old := range l.Records {
			if old.Snapshot.HostSessionID == r.Snapshot.HostSessionID && old.Snapshot.JobID == r.Snapshot.JobID {
				// Progress and terminal snapshots do not carry the private proof.
				// Only PrepareFinalization may replace an existing preparation.
				if old.PreparedFinalization != nil {
					if r.PreparedFinalization != nil && *r.PreparedFinalization != *old.PreparedFinalization {
						return ErrCorrupt
					}
					r.PreparedFinalization = old.PreparedFinalization
				}
				if err := validateRecord(r); err != nil {
					return err
				}
				l.Records[i] = r
				updated = true
				break
			}
		}
		if !updated {
			l.Records = append(l.Records, r)
		}
		if len(l.Records) > 20 {
			l.Records = l.Records[len(l.Records)-20:]
		}
		if err = s.ensure(); err != nil {
			return err
		}
		return atomicJSON(filepath.Join(s.root, "history.json"), l)
	})
}
func (s *Store) Find(session, job, start string) (found *Record, err error) {
	err = s.locked(func() error {
		l, e := s.read()
		if e != nil {
			return e
		}
		for _, r := range l.Records {
			if r.Snapshot.HostSessionID == session && ((job != "" && r.Snapshot.JobID == job) || (start != "" && r.Snapshot.StartRequestID == start)) {
				copy := r
				found = &copy
				break
			}
		}
		return nil
	})
	return
}
func (s *Store) History() (records []Record, err error) {
	err = s.locked(func() error { l, e := s.read(); records = l.Records; return e })
	return
}
func (s *Store) ClearHistory() error {
	return s.locked(func() error {
		l, err := s.read()
		if err != nil {
			return err
		}
		l.Records = []Record{}
		if err = s.ensure(); err != nil {
			return err
		}
		return atomicJSON(filepath.Join(s.root, "history.json"), l)
	})
}
func (s *Store) read() (ledger, error) {
	empty := ledger{Version: 1, Owner: s.owner, Records: []Record{}}
	if err := hostwin.CheckPath(s.root, true); err != nil {
		return empty, err
	}
	markerPath := filepath.Join(s.root, "owner.json")
	if err := hostwin.CheckPath(markerPath, true); err != nil {
		return empty, err
	}
	marker, err := os.ReadFile(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		entries, e := os.ReadDir(s.root)
		if errors.Is(e, os.ErrNotExist) {
			return empty, nil
		}
		if e != nil {
			return empty, e
		}
		if len(entries) > 0 {
			return empty, ErrCorrupt
		}
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var mark ownerMark
	if strictJSON(marker, &mark) != nil || mark.Version != 1 || mark.Owner != s.owner {
		return empty, ErrCorrupt
	}
	path := filepath.Join(s.root, "history.json")
	if err = hostwin.CheckPath(path, true); err != nil {
		return empty, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var l ledger
	if len(b) > contracts.MaxMessageBytes || strictJSON(b, &l) != nil || l.Version != 1 || l.Owner != s.owner || l.Records == nil || len(l.Records) > 20 {
		return empty, ErrCorrupt
	}
	seen := map[string]bool{}
	for _, r := range l.Records {
		key := r.Snapshot.HostSessionID + ":" + r.Snapshot.JobID
		if seen[key] || validateRecord(r) != nil {
			return empty, ErrCorrupt
		}
		seen[key] = true
	}
	return l, nil
}
func (s *Store) ensure() error {
	if err := hostwin.CreateDirectories(s.root, true); err != nil {
		return err
	}
	markerPath := filepath.Join(s.root, "owner.json")
	if _, err := os.Stat(markerPath); errors.Is(err, os.ErrNotExist) {
		return atomicJSON(markerPath, ownerMark{1, s.owner})
	} else {
		return err
	}
}
func strictJSON(b []byte, out any) error {
	if !utf8.Valid(b) {
		return ErrCorrupt
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return ErrCorrupt
	}
	return nil
}
func validateRecord(r Record) error {
	if !privateText(r.Fingerprint, 128) {
		return ErrCorrupt
	}
	state := r.Snapshot.State
	terminal := state == contracts.StateCompleted || state == contracts.StateCanceled || state == contracts.StateFailed || state == contracts.StateInterrupted
	if r.TerminalConfirmed && !terminal {
		return ErrCorrupt
	}
	if err := validatePrivateSnapshot(r.Snapshot); err != nil {
		return ErrCorrupt
	}
	if r.PreparedFinalization != nil {
		if validatePreparation(*r.PreparedFinalization) != nil {
			return ErrCorrupt
		}
		if r.Snapshot.CompletedFile != nil && *r.Snapshot.CompletedFile != r.PreparedFinalization.File {
			return ErrCorrupt
		}
	}
	return nil
}

// Validate the actual snapshot using its existing protocol bounds and codes;
// private preparation fields never enter the browser protocol validator.
func validatePrivateSnapshot(snapshot contracts.JobSnapshot) error {
	result, err := json.Marshal(contracts.StatusResult{Snapshot: &snapshot})
	if err != nil {
		return ErrCorrupt
	}
	b, err := json.Marshal(contracts.Response{ProtocolVersion: 1, Kind: "response", RequestID: "ledger-check", HostSessionID: snapshot.HostSessionID, Operation: contracts.OperationStatus, Result: result})
	if err != nil {
		return ErrCorrupt
	}
	if _, err = contracts.DecodeMessage(b); err != nil {
		return ErrCorrupt
	}
	return nil
}
func atomicJSON(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > contracts.MaxMessageBytes {
		return ErrCorrupt
	}
	unlock, err := hostwin.LockPath(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unlock()
	if err = hostwin.CheckPath(path, true); err != nil {
		return err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	tmp := path + "." + hex.EncodeToString(random[:]) + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err = file.Write(b); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func VerifyCompleted(c contracts.CompletedFile) error {
	if c.SizeBytes == 0 || len(c.SHA256) != 64 {
		return errors.New("invalid output summary")
	}
	unlock, err := hostwin.LockPath(c.Path, false)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.Open(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || uint64(stat.Size()) != c.SizeBytes {
		return errors.New("output size differs")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != c.SHA256 {
		return errors.New("output hash differs")
	}
	return nil
}
