// SPDX-License-Identifier: MPL-2.0

// Package composite selects the history driver of a running registry.
package composite

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/registry"
)

// Driver is the capability set that registry operations use. Every driver
// behind a History must implement all of it.
type Driver interface {
	registry.ResolutionHeadCASHistory
	registry.HeadCASHistory
	registry.ChangeSetReplayer
	registry.VersionLookup
	registry.VersionIDBounds
}

// History forwards each call to the active driver. A registry transaction
// keeps the same driver until it ends. Switch waits for transactions in
// progress and blocks new transactions until the new driver is active.
type History struct {
	active     atomic.Pointer[active]
	baseline   registry.State
	previous   []Driver
	transition sync.RWMutex
	baselineMu sync.RWMutex
	hasBase    bool
}

type active struct{ driver Driver }

var (
	_ Driver                        = (*History)(nil)
	_ registry.TransactionalHistory = (*History)(nil)
	_ registry.BaselineHistory      = (*History)(nil)
)

func New(driver Driver) *History {
	h := &History{}
	h.active.Store(&active{driver: driver})
	return h
}

// Active returns the active driver.
func (h *History) Active() Driver { return h.active.Load().driver }

func (h *History) BeginTransaction() func() {
	h.transition.RLock()
	return h.transition.RUnlock
}

// Switch runs prepare with the active driver and the current baseline while
// no registry transaction runs. When prepare succeeds, its driver becomes
// active. The previous driver stays open for readers until Close.
func (h *History) Switch(prepare func(current Driver, baseline registry.State) (Driver, error)) error {
	h.transition.Lock()
	defer h.transition.Unlock()
	current := h.Active()
	baseline, err := h.Baseline()
	if err != nil && !errors.Is(err, registry.ErrBaselineNotFound) {
		return err
	}
	next, err := prepare(current, baseline)
	if err != nil {
		return err
	}
	h.active.Store(&active{driver: next})
	h.previous = append(h.previous, current)
	return nil
}

func (h *History) Versions() ([]registry.Version, error) { return h.Active().Versions() }

func (h *History) Get(v registry.Version) (registry.ChangeSet, error) { return h.Active().Get(v) }

func (h *History) Save(v registry.Version, changes registry.ChangeSet, head bool) error {
	return h.Active().Save(v, changes, head)
}

func (h *History) Head() (registry.Version, error) { return h.Active().Head() }

func (h *History) SetHead(v registry.Version) error { return h.Active().SetHead(v) }

func (h *History) ReplayChanges(ctx context.Context, target registry.Version, apply func(registry.ChangeSet) error) error {
	return h.Active().ReplayChanges(ctx, target, apply)
}

func (h *History) MaxVersionID() (uint, error) { return h.Active().MaxVersionID() }

func (h *History) GetVersion(id uint) (registry.Version, error) { return h.Active().GetVersion(id) }

func (h *History) CompareAndSetHead(expected, target registry.Version) error {
	return h.Active().CompareAndSetHead(expected, target)
}

func (h *History) GetDependencyResolution(v registry.Version) (*registry.DependencyResolution, error) {
	return h.Active().GetDependencyResolution(v)
}

func (h *History) SaveWithDependencyResolution(v registry.Version, changes registry.ChangeSet, resolution *registry.DependencyResolution, head bool) error {
	return h.Active().SaveWithDependencyResolution(v, changes, resolution, head)
}

func (h *History) CheckpointDependencyResolution(v registry.Version, resolution *registry.DependencyResolution) error {
	return h.Active().CheckpointDependencyResolution(v, resolution)
}

func (h *History) CompareAndSetHeadWithDependencyResolution(expected, target registry.Version, resolution *registry.DependencyResolution) error {
	return h.Active().CompareAndSetHeadWithDependencyResolution(expected, target, resolution)
}

// Baseline returns the baseline of a driver that stores one. For other drivers
// it returns the last baseline given to SaveBaseline.
func (h *History) Baseline() (registry.State, error) {
	if stored, ok := h.Active().(registry.BaselineHistory); ok {
		return stored.Baseline()
	}
	h.baselineMu.RLock()
	defer h.baselineMu.RUnlock()
	if !h.hasBase {
		return nil, registry.ErrBaselineNotFound
	}
	return append(registry.State(nil), h.baseline...), nil
}

func (h *History) SaveBaseline(state registry.State) error {
	if stored, ok := h.Active().(registry.BaselineHistory); ok {
		return stored.SaveBaseline(state)
	}
	h.baselineMu.Lock()
	defer h.baselineMu.Unlock()
	h.baseline, h.hasBase = append(registry.State(nil), state...), true
	return nil
}

func (h *History) Close() error {
	h.transition.Lock()
	defer h.transition.Unlock()
	var err error
	for _, driver := range append(h.previous, h.Active()) {
		if closer, ok := driver.(io.Closer); ok {
			err = errors.Join(err, closer.Close())
		}
	}
	h.previous = nil
	return err
}
