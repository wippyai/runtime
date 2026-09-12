// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/pid"
)

// reapOwners returns an explicit outcome for every distinct owner. A nil error
// means the authoritative enumeration and its conditional deletes finished.
// The caller must retain owners with errors; one failed owner does not turn a
// successful owner into unfinished work. This function does not own retries.
func (s *Service) reapOwners(ctx context.Context, owners []pid.PID) map[string]error {
	results := make(map[string]error, len(owners))
	unique := make(map[string]pid.PID, len(owners))
	for _, owner := range owners {
		unique[owner.String()] = owner
		results[owner.String()] = nil
	}
	if len(unique) == 0 {
		return results
	}
	failAll := func(err error) map[string]error {
		for key := range results {
			results[key] = err
		}
		return results
	}
	if err := ctx.Err(); err != nil {
		return failAll(err)
	}
	if s.cleanupSnapshot == nil {
		for key, owner := range unique {
			results[key] = s.reapBindingsContext(ctx, pidIndexBase(owner), false)
		}
		return results
	}
	snapshot, err := s.cleanupSnapshot(ctx)
	if err != nil {
		return failAll(err)
	}
	if snapshot == nil {
		return failAll(fmt.Errorf("cleanup authority returned no snapshot"))
	}
	// Validate enumeration before the first mutation. A corrupt/unavailable
	// snapshot cannot be treated as absence for the owners not reached yet.
	type victim struct {
		owner pid.PID
		name  string
	}
	var victims []victim
	for key, entry := range snapshot.Entries {
		if err := ctx.Err(); err != nil {
			return failAll(err)
		}
		if !strings.HasPrefix(key, activePrefix) {
			continue
		}
		active, err := decodeActive(entry.Value)
		if err != nil {
			return failAll(err)
		}
		if activeKey(active.Name) != key {
			return failAll(fmt.Errorf("cleanup active key/name mismatch"))
		}
		owner, err := pid.ParsePID(active.PID)
		if err != nil {
			return failAll(err)
		}
		if _, selected := unique[owner.String()]; selected {
			victims = append(victims, victim{owner: owner, name: active.Name})
		}
	}
	remaining := make(map[string]int, len(unique))
	for _, victim := range victims {
		remaining[victim.owner.String()]++
	}
	for _, victim := range victims {
		key := victim.owner.String()
		if err := ctx.Err(); err != nil {
			// Keep completed owners complete; retry only the unfinished portion.
			for owner, count := range remaining {
				if count > 0 {
					results[owner] = errors.Join(results[owner], err)
				}
			}
			return results
		}
		results[key] = errors.Join(results[key], s.deleteBindingWithPolicyContext(ctx, victim.owner, victim.name, false))
		remaining[key]--
	}
	return results
}
