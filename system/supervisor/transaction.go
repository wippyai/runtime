// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"sort"

	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

type regTx struct {
	register map[string]*supervisor.Entry
	remove   map[string]struct{}
	logger   *zap.Logger
	open     bool
}

func newRegTx(logger *zap.Logger) *regTx {
	return &regTx{
		register: make(map[string]*supervisor.Entry),
		remove:   make(map[string]struct{}),
		logger:   logger,
	}
}

func (th *regTx) begin() {
	if th.open {
		th.logger.Warn("received begin transaction while already in transaction, resetting state")
	}

	th.open = true
	th.register = make(map[string]*supervisor.Entry)
	th.remove = make(map[string]struct{})
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

	delete(th.remove, id)
	th.register[id] = entry // always use the latest entry
	return nil
}

func (th *regTx) removeService(id string) error {
	if !th.open {
		return supervisor.ErrOutsideTransaction
	}

	// duplicate check
	if _, exists := th.remove[id]; exists {
		return nil
	}

	delete(th.register, id)
	th.remove[id] = struct{}{}

	return nil
}

func (th *regTx) reset() {
	th.open = false
	th.register = make(map[string]*supervisor.Entry)
	th.remove = make(map[string]struct{})
}
