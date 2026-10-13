package storage

import (
	"context"
	"crypto/rand"
	"dwnl/host/internal/contracts"
	"dwnl/host/internal/windows"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
)

type FolderPicker func(context.Context) (path string, canceled bool, err error)
type Destinations struct {
	mu      sync.Mutex
	paths   map[string]string
	current contracts.Destination
	picker  FolderPicker
}

func NewDestinations(defaultPath string, picker FolderPicker) (*Destinations, error) {
	path, err := filepath.Abs(defaultPath)
	if err != nil {
		return nil, err
	}
	if err = windows.CheckPath(path, true); err != nil {
		return nil, err
	}
	if picker == nil {
		picker = windows.ChooseFolder
	}
	d := &Destinations{paths: map[string]string{}, picker: picker}
	if _, err = d.register(path); err != nil {
		return nil, err
	}
	return d, nil
}
func (d *Destinations) register(path string) (contracts.Destination, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return contracts.Destination{}, err
	}
	id := "destination-" + hex.EncodeToString(random[:])
	dest := contracts.Destination{DestinationID: id, Label: path}
	d.paths[id] = path
	d.current = dest
	return dest, nil
}
func (d *Destinations) Default() contracts.Destination {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current
}
func (d *Destinations) Choose(ctx context.Context) (contracts.ChooseDestinationResult, error) {
	path, canceled, err := d.picker(ctx)
	if err != nil {
		return contracts.ChooseDestinationResult{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	dest := d.current
	if !canceled {
		path, err = filepath.Abs(path)
		if err != nil {
			return contracts.ChooseDestinationResult{}, err
		}
		if err = windows.CheckPath(path, false); err != nil {
			return contracts.ChooseDestinationResult{}, err
		}
		dest, err = d.register(path)
		if err != nil {
			return contracts.ChooseDestinationResult{}, err
		}
	}
	return contracts.ChooseDestinationResult{DestinationID: dest.DestinationID, Label: dest.Label, Canceled: canceled}, nil
}
func (d *Destinations) Resolve(id string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	path, ok := d.paths[id]
	if !ok {
		return "", errors.New("destination is not approved in this session")
	}
	if err := windows.CreateDirectories(path, false); err != nil {
		return "", err
	}
	return path, nil
}
