// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wippyai/go-lua/types/diag"
)

const (
	metaFile     = "meta.json"
	manifestFile = "manifest.bin"
	diagsFile    = "diags.json"
	protoFile    = "proto.luac"
)

// DiskStore stores cached Lua artifacts and coordinates writes with eviction.
type DiskStore struct {
	pruner *diskPruner
	writer atomicFileWriter
	root   string
	mu     sync.RWMutex
}

// NewDiskStore creates a disk-backed cache store.
func NewDiskStore(dir string) *DiskStore {
	return NewBoundedDiskStore(dir, DefaultMaxBytes, DefaultMaxEntries, DefaultPruneInterval)
}

// NewBoundedDiskStore creates a disk cache with bounded retained generations.
func NewBoundedDiskStore(dir string, maxBytes int64, maxEntries, pruneInterval int) *DiskStore {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	if pruneInterval <= 0 {
		pruneInterval = DefaultPruneInterval
	}
	store := &DiskStore{root: dir, writer: atomicFileWriter{rename: os.Rename}}
	store.pruner = newDiskPruner(dir, maxBytes, maxEntries, pruneInterval, &store.mu)
	return store
}

// Delete removes a cache entry by key.
func (s *DiskStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.RemoveAll(s.entryDir(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Get retrieves a cache entry by key.
func (s *DiskStore) Get(key string) (*Entry, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	if meta.ManifestHash != "" {
		data, err := os.ReadFile(filepath.Join(entryDir, manifestFile))
		if err != nil || hashBytes(data) != meta.ManifestHash {
			return nil, false, nil
		}
		entry.Manifest = data
	}

	if meta.DiagnosticsHash != "" {
		data, err := os.ReadFile(filepath.Join(entryDir, diagsFile))
		if err != nil || hashBytes(data) != meta.DiagnosticsHash {
			return nil, false, nil
		}
		var diags []diag.Diagnostic
		if err := json.Unmarshal(data, &diags); err != nil {
			return nil, false, nil
		}
		entry.Diagnostics = diags
	}

	if meta.ProtoHash != "" {
		data, err := os.ReadFile(filepath.Join(entryDir, protoFile))
		if err != nil || hashBytes(data) != meta.ProtoHash {
			return nil, false, nil
		}
		entry.Proto = data
	}

	if _, err := os.Stat(metaPath); err != nil {
		return nil, false, nil
	}

	return entry, true, nil
}

// Put writes a cache entry by key.
func (s *DiskStore) Put(key string, entry *Entry) error {
	if entry == nil {
		return nil
	}
	s.mu.RLock()
	err := s.put(key, entry)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	s.pruner.recordWrite()
	return nil
}

// SetPruneErrorHandler sets a callback for background cleanup failures.
// The callback runs on the prune worker and must return quickly.
func (s *DiskStore) SetPruneErrorHandler(fn func(error)) {
	s.pruner.worker.setErrorHandler(fn)
}

// WaitPrune waits for background cleanup. Call it after the writes it should cover.
func (s *DiskStore) WaitPrune() {
	s.pruner.worker.wait()
}

// Prune synchronously applies the retention policy and reports incomplete scans.
func (s *DiskStore) Prune() error {
	return s.pruner.prune()
}

func (s *DiskStore) put(key string, entry *Entry) error {
	entryDir := s.entryDir(key)
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		return err
	}

	meta := entry.Meta
	meta.SchemaVersion = SchemaVersion
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = nowUTC()
	}

	if len(entry.Manifest) > 0 {
		meta.ManifestHash = hashBytes(entry.Manifest)
		if err := s.writer.write(entryDir, manifestFile, entry.Manifest); err != nil {
			return err
		}
	} else {
		meta.ManifestHash = ""
		_ = os.Remove(filepath.Join(entryDir, manifestFile))
	}

	if len(entry.Diagnostics) > 0 {
		data, err := json.Marshal(entry.Diagnostics)
		if err != nil {
			return err
		}
		meta.DiagnosticsHash = hashBytes(data)
		if err := s.writer.write(entryDir, diagsFile, data); err != nil {
			return err
		}
	} else {
		meta.DiagnosticsHash = ""
		_ = os.Remove(filepath.Join(entryDir, diagsFile))
	}

	if len(entry.Proto) > 0 {
		meta.ProtoHash = hashBytes(entry.Proto)
		if err := s.writer.write(entryDir, protoFile, entry.Proto); err != nil {
			return err
		}
	} else {
		meta.ProtoHash = ""
		_ = os.Remove(filepath.Join(entryDir, protoFile))
	}

	metaData, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	return s.writer.write(entryDir, metaFile, metaData)
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (s *DiskStore) entryDir(key string) string {
	return filepath.Join(s.root, "v1", "entries", key)
}

var nowUTC = func() time.Time { return time.Now().UTC() }
