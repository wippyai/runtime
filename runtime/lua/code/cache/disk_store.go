// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
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
	root          string
	maxBytes      int64
	maxEntries    int
	pruneInterval uint64
	writes        atomic.Uint64
	mu            sync.RWMutex
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
	return &DiskStore{
		root: dir, maxBytes: maxBytes, maxEntries: maxEntries,
		pruneInterval: uint64(pruneInterval),
	}
}

// Delete removes a cache entry by key.
func (s *DiskStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.entryDir(key))
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
	s.mu.RLock()
	err := s.put(key, entry)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	if s.writes.Add(1)%s.pruneInterval == 0 {
		return s.Prune()
	}
	return nil
}

func (s *DiskStore) put(key string, entry *Entry) error {
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

type diskEntryInfo struct {
	created time.Time
	path    string
	size    int64
}

// Prune removes the oldest retained generations until both configured limits
// are satisfied. Cache eviction never affects correctness: a miss recompiles.
func (s *DiskStore) Prune() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	root := filepath.Join(s.root, "v1", "entries")
	dirs, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	entries := make([]diskEntryInfo, 0, len(dirs))
	var total int64
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		path := filepath.Join(root, dir.Name())
		info := diskEntryInfo{path: path}
		walkErr := filepath.WalkDir(path, func(_ string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type().IsRegular() {
				stat, statErr := d.Info()
				if statErr != nil {
					return statErr
				}
				info.size += stat.Size()
				if info.created.IsZero() || stat.ModTime().Before(info.created) {
					info.created = stat.ModTime()
				}
			}
			return nil
		})
		if walkErr != nil {
			return walkErr
		}
		if data, readErr := os.ReadFile(filepath.Join(path, metaFile)); readErr == nil {
			var meta Meta
			if json.Unmarshal(data, &meta) == nil && !meta.CreatedAt.IsZero() {
				info.created = meta.CreatedAt
			}
		}
		total += info.size
		entries = append(entries, info)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].created.Equal(entries[j].created) {
			return entries[i].path < entries[j].path
		}
		return entries[i].created.Before(entries[j].created)
	})
	for len(entries) > s.maxEntries || total > s.maxBytes {
		oldest := entries[0]
		entries = entries[1:]
		if err := os.RemoveAll(oldest.path); err != nil {
			return err
		}
		total -= oldest.size
	}
	return nil
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
