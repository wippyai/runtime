// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"errors"
	api "github.com/wippyai/runtime/api/service/cdc"
)

// stopUnstartedSource abandons a source returned by Driver.Create before its
// Start method has successfully handed ownership of a durable resource to the
// manager. Drivers must make Create side-effect-free; Stop is intentionally
// the only cleanup allowed on this path so an unstarted candidate cannot drop
// a shared replication slot/checkpoint.
func stopUnstartedSource(ctx context.Context, source api.Source) error {
	return stopSource(ctx, source)
}

// cleanupStartedSource cleans a candidate after Start was attempted. A
// different exclusive resource is owned solely by that candidate and may be
// destructively disposed. Same-key candidates share the old resource contract
// and must only be stopped; disposing them could drop the old generation's
// resource.
func cleanupStartedSource(ctx context.Context, source api.Source, destructive bool) error {
	if destructive {
		return disposeSource(ctx, source)
	}
	return stopSource(ctx, source)
}

// cleanupCandidate performs the only cleanup of a private replacement
// generation. When cleanup itself fails, the candidate is retained in the
// slot's retired queue so Stop/Delete can retry it. A different resource keeps
// its candidate lease; a same-key candidate never owns the old lease and must
// not release it when its non-destructive Stop eventually succeeds.
func (s *sourceSlot) cleanupCandidate(
	ctx context.Context,
	source ManagedSource,
	lease leaseRef,
	differentResource bool,
	started bool,
) error {
	if isNilSource(source) {
		return nil
	}
	var err error
	if started {
		err = cleanupStartedSource(ctx, source, differentResource)
	} else {
		err = stopUnstartedSource(ctx, source)
	}
	if err == nil {
		return nil
	}
	if !differentResource && !lease.owned {
		lease = leaseRef{}
	}
	s.recordRetired(source, lease.key, lease.token, started && differentResource)
	return err
}

func disposeSource(ctx context.Context, source api.Source) error {
	if isNilSource(source) {
		return nil
	}
	if disposable, ok := source.(Disposable); ok {
		return disposable.Dispose(ctx)
	}
	return stopSource(ctx, source)
}

func stopGeneration(ctx context.Context, source api.Source, cancel context.CancelFunc) error {
	if cancel != nil {
		cancel()
	}
	return stopSource(ctx, source)
}

func (s *sourceSlot) recordRetired(source ManagedSource, key string, token uint64, destructive bool) {
	if isNilSource(source) {
		return
	}
	s.mu.Lock()
	for _, existing := range s.retired {
		if existing.source == source {
			s.mu.Unlock()
			return
		}
	}
	s.retired = append(s.retired, retiredSource{
		source:      source,
		key:         key,
		token:       token,
		destructive: destructive,
	})
	s.mu.Unlock()
}

func (s *sourceSlot) isRetiredLocked(source ManagedSource) bool {
	if isNilSource(source) {
		return false
	}
	for _, retired := range s.retired {
		if retired.source == source {
			return true
		}
	}
	return false
}

func (s *sourceSlot) hasRetiredSource(source ManagedSource) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, retired := range s.retired {
		if retired.source == source {
			return true
		}
	}
	return false
}

func (s *sourceSlot) retryRetired(ctx context.Context) error {
	s.mu.RLock()
	retired := append([]retiredSource(nil), s.retired...)
	s.mu.RUnlock()
	var errs []error
	for _, item := range retired {
		var err error
		if item.destructive {
			err = disposeSource(ctx, item.source)
		} else {
			err = stopSource(ctx, item.source)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}

		s.mu.Lock()
		for i, current := range s.retired {
			if current.source == item.source {
				s.retired = append(s.retired[:i], s.retired[i+1:]...)
				break
			}
		}
		hook := s.retiredHook
		s.mu.Unlock()
		if hook != nil && item.key != "" {
			hook(item.key, item.token)
		}
	}
	return errors.Join(errs...)
}

func (s *sourceSlot) setRetiredCleanupHook(hook func(string, uint64)) {
	s.mu.Lock()
	s.retiredHook = hook
	s.mu.Unlock()
}

func (s *sourceSlot) hasRetiredKey(key string) bool {
	if key == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, retired := range s.retired {
		if retired.key == key {
			return true
		}
	}
	return false
}

func (s *sourceSlot) resourceKeys() []string {
	s.mu.RLock()
	current := s.current
	retired := append([]retiredSource(nil), s.retired...)
	s.mu.RUnlock()
	keys := make([]string, 0, len(retired)+1)
	if key := exclusiveResourceKey(current); key != "" {
		keys = append(keys, key)
	}
	for _, item := range retired {
		if item.key == "" {
			continue
		}
		seen := false
		for _, key := range keys {
			if key == item.key {
				seen = true
				break
			}
		}
		if !seen {
			keys = append(keys, item.key)
		}
	}
	return keys
}
