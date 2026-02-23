// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
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

	// Apply all tx changes
	for id := range th.remove {
		if err := removeFn(id); err != nil {
			return NewCommitRemoveError(id, err)
		}
	}

	for id, entry := range th.register {
		if err := registerFn(id, entry); err != nil {
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
