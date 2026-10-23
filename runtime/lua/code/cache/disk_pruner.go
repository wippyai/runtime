// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// diskPruner owns disk-cache retention and schedules cleanup after successful writes.
// The scan runs outside storeMu; eviction takes it only to recheck and remove one entry.
type diskPruner struct {
	scan       func(string) (diskEntryInfo, error)
	removeAll  func(string) error
	storeMu    *sync.RWMutex
	root       string
	worker     pruneWorker
	maxBytes   int64
	maxEntries int
	interval   uint64
	writes     atomic.Uint64
	pruneMu    sync.Mutex
}

func newDiskPruner(root string, maxBytes int64, maxEntries, interval int, storeMu *sync.RWMutex) *diskPruner {
	p := &diskPruner{
		root: root, maxBytes: maxBytes, maxEntries: maxEntries,
		interval: uint64(interval), storeMu: storeMu,
		scan: scanEntry, removeAll: os.RemoveAll,
	}
	p.worker.prune = p.prune
	return p
}

func (p *diskPruner) recordWrite() {
	if p.writes.Add(1)%p.interval == 0 {
		p.worker.request()
	}
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

var (
	errOverLimits     = errors.New("lua cache is still over its limits after pruning")
	errIncompleteScan = errors.New("lua cache byte usage could not be fully measured")
)

// prune removes the oldest retained generations until both configured limits
// are satisfied. Cache eviction never affects correctness: a miss recompiles.
//
// The scan holds no store lock, so Get and Put continue while it walks the
// directory. An entry that cannot be scanned or removed is skipped, so one
// stuck entry cannot stop eviction. Unscanned entries count toward the entry
// limit and make the byte total incomplete, which is reported as an error.
// Each eviction rechecks its entry under the store lock to preserve rewrites.
func (p *diskPruner) prune() error {
	p.pruneMu.Lock()
	defer p.pruneMu.Unlock()

	root := filepath.Join(p.root, "v1", "entries")
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
		info, scanErr := p.scan(path)
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
		if count <= p.maxEntries && total <= p.maxBytes {
			break
		}
		removed, removeErr := p.removeEntryDir(victim)
		if removeErr != nil {
			addErr(removeErr)
			continue
		}
		if removed {
			count--
			total -= victim.size
		}
	}
	var outcome []error
	if count > p.maxEntries || total > p.maxBytes {
		outcome = append(outcome, fmt.Errorf("%w: %d entries (limit %d), %d measured bytes (limit %d), %d failed operations",
			errOverLimits, count, p.maxEntries, total, p.maxBytes, failed))
	}
	if unscanned > 0 {
		outcome = append(outcome, fmt.Errorf("%w: %d unscanned entries", errIncompleteScan, unscanned))
	}
	if len(outcome) > 0 {
		return errors.Join(append(outcome, errs...)...)
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
	err := filepath.WalkDir(path, func(_ string, d os.DirEntry, walkErr error) error {
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
func (p *diskPruner) removeEntryDir(victim diskEntryInfo) (bool, error) {
	p.storeMu.Lock()
	defer p.storeMu.Unlock()
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
	if err := p.removeAll(victim.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}
