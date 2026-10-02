// SPDX-License-Identifier: MPL-2.0

package cache

import "sync"

// LayeredStore reads authenticated embedded entries before mutable disk entries.
// Writes and pruning belong to the mutable store. Deleting a rejected artifact
// masks its embedded key for this manager so recompilation can replace it.
type LayeredStore struct {
	embedded Reader
	writable Store
	rejected map[string]bool
	mu       sync.RWMutex
}

// NewLayeredStore overlays an immutable reader on an ordinary cache store.
func NewLayeredStore(embedded Reader, writable Store) *LayeredStore {
	return &LayeredStore{embedded: embedded, writable: writable, rejected: make(map[string]bool)}
}

func (s *LayeredStore) Get(key string) (*Entry, bool, error) {
	s.mu.RLock()
	rejected := s.rejected[key]
	s.mu.RUnlock()
	if !rejected {
		if entry, ok, err := s.embedded.Get(key); err == nil && ok {
			return entry, true, nil
		}
	}
	return s.writable.Get(key)
}

func (s *LayeredStore) Put(key string, entry *Entry) error {
	if err := s.writable.Put(key, entry); err != nil {
		return err
	}
	s.mu.Lock()
	s.rejected[key] = true
	s.mu.Unlock()
	return nil
}

func (s *LayeredStore) Delete(key string) error {
	s.mu.Lock()
	s.rejected[key] = true
	s.mu.Unlock()
	if deleter, ok := s.writable.(Deleter); ok {
		return deleter.Delete(key)
	}
	return nil
}

func (s *LayeredStore) Prune() error {
	if pruner, ok := s.writable.(Pruner); ok {
		return pruner.Prune()
	}
	return nil
}
