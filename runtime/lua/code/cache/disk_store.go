// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/go-lua/types/diag"
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

	meta := entry.Meta
	meta.SchemaVersion = SchemaVersion
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = nowUTC()
	}

	if len(entry.Manifest) > 0 {
		meta.ManifestHash = hashBytes(entry.Manifest)
		if err := writeFileAtomic(entryDir, manifestFile, entry.Manifest); err != nil {
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
		if err := writeFileAtomic(entryDir, diagsFile, data); err != nil {
			return err
		}
	} else {
		meta.DiagnosticsHash = ""
		_ = os.Remove(filepath.Join(entryDir, diagsFile))
	}

	if len(entry.Proto) > 0 {
		meta.ProtoHash = hashBytes(entry.Proto)
		if err := writeFileAtomic(entryDir, protoFile, entry.Proto); err != nil {
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

	return writeFileAtomic(entryDir, metaFile, metaData)
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
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
				if errors.Is(walkErr, os.ErrNotExist) {
					return nil
				}
				return walkErr
			}
			if d.Type().IsRegular() {
				if strings.Contains(d.Name(), ".tmp-") {
					return nil
				}
				stat, statErr := d.Info()
				if statErr != nil {
					if errors.Is(statErr, os.ErrNotExist) {
						return nil
					}
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
			if errors.Is(walkErr, os.ErrNotExist) {
				continue
			}
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
		if err := os.RemoveAll(oldest.path); err != nil && !errors.Is(err, os.ErrNotExist) {
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
