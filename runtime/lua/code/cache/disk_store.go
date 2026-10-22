// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
//
// Put never prunes inline. A prune walks every retained entry, and the cache
// directory is shared by every application on the machine, so on a large cold
// cache one walk takes minutes. Put runs on the compile path of registry
// listeners, where a walk that long stalls boot past the registry event wait.
type DiskStore struct {
	pruneErrors    atomic.Pointer[func(error)]
	root           string
	maxBytes       int64
	maxEntries     int
	pruneInterval  uint64
	writes         atomic.Uint64
	mu             sync.RWMutex
	background     sync.WaitGroup
	pruneMu        sync.Mutex
	pruning        atomic.Bool
	pruneRequested atomic.Bool
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
		s.pruneInBackground()
	}
	return nil
}

// SetPruneErrorHandler sets the function that receives the errors of
// background prunes started by Put. Put itself never returns them. The handler
// runs on the prune goroutine and must return quickly.
func (s *DiskStore) SetPruneErrorHandler(fn func(error)) {
	if fn == nil {
		s.pruneErrors.Store(nil)
		return
	}
	s.pruneErrors.Store(&fn)
}

// pruneInBackground runs at most one prune goroutine. A request that arrives
// while it runs makes it prune once more, so the newest writes are counted.
// Background prunes are best effort: a failed eviction leaves extra entries
// for the next prune, and a miss only recompiles.
func (s *DiskStore) pruneInBackground() {
	s.pruneRequested.Store(true)
	if !s.pruning.CompareAndSwap(false, true) {
		return
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		for {
			s.pruneRequested.Store(false)
			if err := s.Prune(); err != nil {
				if handler := s.pruneErrors.Load(); handler != nil {
					(*handler)(err)
				}
			}
			s.pruning.Store(false)
			if !s.pruneRequested.Load() || !s.pruning.CompareAndSwap(false, true) {
				return
			}
		}
	}()
}

// WaitPrune blocks until background prunes started by Put have finished.
// Call it after the writes it should cover have returned.
func (s *DiskStore) WaitPrune() {
	s.background.Wait()
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
	// stamp changes whenever the entry's files change.
	stamp string
	size  int64
}

// maxPruneErrors bounds the errors one pass reports, so a cache directory that
// rejects every removal does not produce one error per entry.
const maxPruneErrors = 8

// errOverLimits marks a prune pass that ended with the cache over its limits.
var errOverLimits = errors.New("lua cache is still over its limits after pruning")

// Prune removes the oldest retained generations until both configured limits
// are satisfied. Cache eviction never affects correctness: a miss recompiles.
//
// The scan holds no store lock, so Get and Put continue while it walks the
// directory. An entry that cannot be scanned or removed is skipped, so one
// stuck entry cannot stop eviction; an entry that cannot be scanned still
// counts toward the entry limit. Prune returns an error, wrapping
// errOverLimits and the first failures, only when the cache is still over its
// limits at the end. Each removal takes the write lock and first scans its
// entry again, so an entry rewritten or completed after the scan is kept.
func (s *DiskStore) Prune() error {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()

	root := filepath.Join(s.root, "v1", "entries")
	dirs, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	failed := 0
	addErr := func(err error) {
		failed++
		if len(errs) < maxPruneErrors {
			errs = append(errs, err)
		}
	}
	entries := make([]diskEntryInfo, 0, len(dirs))
	unscanned := 0
	var total int64
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		path := filepath.Join(root, dir.Name())
		info, scanErr := scanEntry(path)
		pruneScanHook(path)
		if scanErr != nil {
			if !errors.Is(scanErr, os.ErrNotExist) {
				unscanned++
				addErr(scanErr)
			}
			continue
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
	count := len(entries) + unscanned
	for _, victim := range entries {
		if count <= s.maxEntries && total <= s.maxBytes {
			break
		}
		removed, removeErr := s.removeEntryDir(victim)
		if removeErr != nil {
			addErr(removeErr)
			continue
		}
		if removed {
			count--
			total -= victim.size
		}
	}
	if count > s.maxEntries || total > s.maxBytes {
		overLimits := fmt.Errorf("%w: %d entries (limit %d), %d bytes (limit %d), %d failed operations",
			errOverLimits, count, s.maxEntries, total, s.maxBytes, failed)
		return errors.Join(append([]error{overLimits}, errs...)...)
	}
	return nil
}

// scanEntry measures one entry directory. A complete entry is dated by its
// metadata and identified by the metadata's hash, which covers every part. An
// entry without metadata is still being written or was abandoned, so it is
// dated by its latest activity: a write in progress then sorts as new instead
// of as the oldest entry.
func scanEntry(path string) (diskEntryInfo, error) {
	info := diskEntryInfo{path: path}
	var oldest, latest time.Time
	var files int
	var filesBytes int64
	err := walkDir(path, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		stat, statErr := d.Info()
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return nil
			}
			return statErr
		}
		if stat.ModTime().After(latest) {
			latest = stat.ModTime()
		}
		if !d.Type().IsRegular() {
			return nil
		}
		files++
		filesBytes += stat.Size()
		if strings.Contains(d.Name(), ".tmp-") {
			return nil
		}
		info.size += stat.Size()
		if oldest.IsZero() || stat.ModTime().Before(oldest) {
			oldest = stat.ModTime()
		}
		return nil
	})
	if err != nil {
		return info, err
	}
	data, err := os.ReadFile(filepath.Join(path, metaFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
		info.created = latest
		info.stamp = fmt.Sprintf("activity:%d:%d:%d", latest.UnixNano(), files, filesBytes)
	case err != nil:
		// Unreadable metadata: date the entry by its files, as for corrupt
		// metadata, so it still counts and can be evicted.
		info.created = oldest
		info.stamp = fmt.Sprintf("activity:%d:%d:%d", latest.UnixNano(), files, filesBytes)
	default:
		info.stamp = "meta:" + hashBytes(data)
		var meta Meta
		if json.Unmarshal(data, &meta) == nil && !meta.CreatedAt.IsZero() {
			info.created = meta.CreatedAt
		} else {
			info.created = oldest
		}
	}
	return info, nil
}

// removeEntryDir removes a scanned entry unless it changed after the scan, and
// reports whether the entry is gone.
func (s *DiskStore) removeEntryDir(victim diskEntryInfo) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := scanEntry(victim.path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if current.stamp != victim.stamp {
		return false, nil
	}
	if err := removeAll(victim.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
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
	if err := renameReplacing(path, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// renameRetryDelays bound how long publication waits for a reader on Windows.
// Go opens files there without delete sharing, so replacing a file that a Get
// or a prune scan has open fails with "Access is denied" until the reader
// closes it, which takes milliseconds.
var renameRetryDelays = []time.Duration{
	2 * time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 200 * time.Millisecond,
}

func renameReplacing(from, to string) error {
	err := rename(from, to)
	if runtime.GOOS != "windows" {
		return err
	}
	for _, delay := range renameRetryDelays {
		if err == nil || !errors.Is(err, os.ErrPermission) {
			return err
		}
		time.Sleep(delay)
		err = rename(from, to)
	}
	return err
}

var nowUTC = func() time.Time { return time.Now().UTC() }

// Test seams: pruneScanHook runs after the scan reads each entry, outside the
// store lock; walkDir, removeAll and rename let tests inject or observe file
// system failures.
var (
	pruneScanHook = func(string) {}
	walkDir       = filepath.WalkDir
	removeAll     = os.RemoveAll
	rename        = os.Rename
)
