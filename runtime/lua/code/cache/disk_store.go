// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	metaFile     = "meta.json"
	manifestFile = "manifest.bin"
	diagsFile    = "diags.json"
	protoFile    = "proto.luac"
)

// DiskStore stores cache entries on disk.
type DiskStore struct {
	root string
}

// NewDiskStore creates a disk-backed cache store.
func NewDiskStore(dir string) *DiskStore {
	return &DiskStore{root: dir}
}

// Delete removes a cache entry by key.
func (s *DiskStore) Delete(key string) error {
	return os.RemoveAll(s.entryDir(key))
}

// Get retrieves a cache entry by key.
func (s *DiskStore) Get(key string) (*Entry, bool, error) {
	entryDir := s.entryDir(key)
	metaPath := filepath.Join(entryDir, metaFile)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var meta Meta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, false, nil
	}

	entry := &Entry{Meta: meta}
	if data, err := os.ReadFile(filepath.Join(entryDir, manifestFile)); err == nil {
		entry.Manifest = data
	}
	if data, err := os.ReadFile(filepath.Join(entryDir, diagsFile)); err == nil {
		if len(data) > 0 {
			_ = json.Unmarshal(data, &entry.Diagnostics)
		}
	}
	if data, err := os.ReadFile(filepath.Join(entryDir, protoFile)); err == nil {
		entry.Proto = data
	}

	return entry, true, nil
}

// Put writes a cache entry by key.
func (s *DiskStore) Put(key string, entry *Entry) error {
	if entry == nil {
		return nil
	}
	entryDir := s.entryDir(key)
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		return err
	}

	if len(entry.Manifest) > 0 {
		if err := writeFileAtomic(entryDir, manifestFile, entry.Manifest); err != nil {
			return err
		}
	}

	if len(entry.Diagnostics) > 0 {
		data, err := json.Marshal(entry.Diagnostics)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(entryDir, diagsFile, data); err != nil {
			return err
		}
	}

	if len(entry.Proto) > 0 {
		if err := writeFileAtomic(entryDir, protoFile, entry.Proto); err != nil {
			return err
		}
	}

	meta := entry.Meta
	meta.SchemaVersion = SchemaVersion
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = nowUTC()
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	return writeFileAtomic(entryDir, metaFile, metaData)
}

func (s *DiskStore) entryDir(key string) string {
	return filepath.Join(s.root, "v1", "entries", key)
}

func writeFileAtomic(dir, name string, data []byte) error {
	file, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return err
	}
	path := file.Name()
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return os.Rename(path, filepath.Join(dir, name))
}

var nowUTC = func() time.Time { return time.Now().UTC() }
