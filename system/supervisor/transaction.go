// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"sort"

	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

type regTx struct {
	register               map[string]*supervisor.Entry
	remove                 map[string]struct{}
	registeredBeforeRemove map[string]bool
	logger                 *zap.Logger
	open                   bool
}

func newRegTx(logger *zap.Logger) *regTx {
	return &regTx{
		register:               make(map[string]*supervisor.Entry),
		remove:                 make(map[string]struct{}),
		registeredBeforeRemove: make(map[string]bool),
		logger:                 logger,
	}
}

func (th *regTx) begin() {
	if th.open {
		th.logger.Warn("received begin transaction while already in transaction, resetting state")
	}

	th.open = true
	th.resetChanges()
}

func (th *regTx) commit(removeFn func(string) error, registerFn func(string, *supervisor.Entry) error) error {
	if !th.open {
		th.logger.Warn("received commit without active transaction")
		return nil
	}

	// Iterate the transaction sets in sorted ID order so commit callbacks fire
	// in a stable sequence across runs. Go map iteration is hash-seed randomized
	// and the supervisor relies on this order downstream when scheduling
	// services.
	removeIDs := make([]string, 0, len(th.remove))
	for id := range th.remove {
		removeIDs = append(removeIDs, id)
	}
	sort.Strings(removeIDs)
	for _, id := range removeIDs {
		if err := removeFn(id); err != nil {
			return NewCommitRemoveError(id, err)
		}
	}

	registerIDs := make([]string, 0, len(th.register))
	for id := range th.register {
		registerIDs = append(registerIDs, id)
	}
	sort.Strings(registerIDs)
	for _, id := range registerIDs {
		if err := registerFn(id, th.register[id]); err != nil {
			return NewCommitRegisterError(id, err)
		}
	}

	th.reset()
	return nil
}

func (th *regTx) discard() {
	if !th.open {
		th.logger.Warn("received discard without active transaction")
		return
	}

	if len(th.register) > 0 || len(th.remove) > 0 {
		th.logger.Warn("discarding transaction with pending changes")
	}

	th.reset()
}

func (th *regTx) registerService(id string, entry *supervisor.Entry) error {
	if !th.open {
		return supervisor.ErrOutsideTransaction
	}

	if _, removed := th.remove[id]; removed {
		// A register following a remove is a replacement when the remove was
		// already pending before this transaction saw a registration. Keep both
		// operations so commit stops the old controller before installing the
		// new one. A register/remove/register sequence is a canceled
		// registration and retains the historical cancellation behavior.
		if th.registeredBeforeRemove[id] {
			delete(th.remove, id)
			delete(th.registeredBeforeRemove, id)
		}
	}
	th.register[id] = entry // always use the latest entry
	return nil
}

func (th *regTx) removeService(id string) error {
	if !th.open {
		return supervisor.ErrOutsideTransaction
	}

	// A duplicate remove is idempotent, but a remove after a replacement's
	// register cancels that new registration while retaining removal of the old
	// controller. This preserves the final event in the transaction.
	if _, exists := th.remove[id]; exists {
		delete(th.register, id)
		return nil
	}

	_, registered := th.register[id]
	delete(th.register, id)
	th.remove[id] = struct{}{}
	th.registeredBeforeRemove[id] = registered

	return nil
}

func (th *regTx) reset() {
	th.open = false
	th.resetChanges()
}

func (th *regTx) resetChanges() {
	th.register = make(map[string]*supervisor.Entry)
	th.remove = make(map[string]struct{})
	th.registeredBeforeRemove = make(map[string]bool)
}
