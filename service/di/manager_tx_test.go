// SPDX-License-Identifier: MPL-2.0

package di

import (
	"sync"
	"sync/atomic"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/contract"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	apidi "github.com/wippyai/runtime/api/service/di"
	"github.com/wippyai/runtime/system/eventbus"
)

func definitionEntry(id registry.ID, methods ...string) registry.Entry {
	configs := make([]apidi.MethodConfig, 0, len(methods))
	for _, name := range methods {
		configs = append(configs, apidi.MethodConfig{Name: name})
	}
	return registry.Entry{
		ID:   id,
		Kind: apidi.Definition,
		Data: NewMockPayload(&apidi.DefinitionConfig{Methods: configs}),
	}
}

func bindingEntry(id, contractID registry.ID, methods ...string) registry.Entry {
	bound := make(map[string]string, len(methods))
	for _, name := range methods {
		bound[name] = "app:impl." + name
	}
	return registry.Entry{
		ID:   id,
		Kind: apidi.Binding,
		Data: NewMockPayload(&apidi.BindingConfig{
			Contracts: []apidi.BoundContractConfig{
				{Contract: contractID.String(), Methods: bound},
			},
		}),
	}
}

// seedContract installs a definition and one binding outside any transaction,
// mirroring a live instance's committed state before a module upgrade.
func seedContract(t *testing.T, manager *Manager, defID, bindingID registry.ID, methods ...string) {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	require.NoError(t, manager.Add(ctx, definitionEntry(defID, methods...)))
	require.NoError(t, manager.Add(ctx, bindingEntry(bindingID, defID, methods...)))
}

// TestManager_TransactionAddsMethodToLiveContract is the live-upgrade case:
// one changeset carries a definition update that adds a method together with
// the binding update that binds it. Per-entry each half is invalid against the
// pre-change peer; as a transaction the pair must commit.
func TestManager_TransactionAddsMethodToLiveContract(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")

	t.Run("definition update first", func(t *testing.T) {
		manager, _ := setupDIManagerTest()
		seedContract(t, manager, defID, bindingID, "method1")

		require.NoError(t, manager.Begin(ctx))
		require.NoError(t, manager.Update(ctx, definitionEntry(defID, "method1", "method2")))
		require.NoError(t, manager.Update(ctx, bindingEntry(bindingID, defID, "method1", "method2")))
		require.NoError(t, manager.Commit(ctx))

		assert.Len(t, manager.definitions[defID].Methods, 2)
		assert.Len(t, manager.bindings[bindingID].Contracts[0].Methods, 2)
	})

	t.Run("binding update first", func(t *testing.T) {
		manager, _ := setupDIManagerTest()
		seedContract(t, manager, defID, bindingID, "method1")

		require.NoError(t, manager.Begin(ctx))
		require.NoError(t, manager.Update(ctx, bindingEntry(bindingID, defID, "method1", "method2")))
		require.NoError(t, manager.Update(ctx, definitionEntry(defID, "method1", "method2")))
		require.NoError(t, manager.Commit(ctx))

		assert.Len(t, manager.definitions[defID].Methods, 2)
		assert.Len(t, manager.bindings[bindingID].Contracts[0].Methods, 2)
	})
}

// TestManager_TransactionCommitRejectsIncompleteState: a definition update
// whose bindings never arrive must fail at Commit and leave committed state
// untouched, with no contract-plane event emitted.
func TestManager_TransactionCommitRejectsIncompleteState(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")
	seedContract(t, manager, defID, bindingID, "method1")

	var updates atomic.Int32
	sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, contract.UpdateDefinition, func(event.Event) {
		updates.Add(1)
	})
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Update(ctx, definitionEntry(defID, "method1", "method2")))
	err = manager.Commit(ctx)
	requireAPIError(t, err, apierror.Invalid, "contract method is not bound")

	assert.Len(t, manager.definitions[defID].Methods, 1)
	assert.NotNil(t, manager.tx, "staging must survive until the runner finishes rollback")
	assert.Equal(t, int32(0), updates.Load())

	// The runner discards after it has dispatched rollback operations.
	require.NoError(t, manager.Discard(ctx))
	assert.Nil(t, manager.tx)
}

// TestManager_FailedCommitAcceptsRollbackBeforeDiscard covers the runner's
// commit-failure protocol: inverse entry operations are dispatched before
// TxDiscard. A failed Commit must therefore retain staging so those inverses
// are accepted against the staged view; Discard then drops the whole attempt.
func TestManager_FailedCommitAcceptsRollbackBeforeDiscard(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, _ := setupDIManagerTest()
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Add(ctx, bindingEntry(bindingID, defID, "method1")))
	err := manager.Commit(ctx)
	requireAPIError(t, err, apierror.Invalid, "binding references undefined contract")

	// This is the inverse of the accepted EntryCreate sent by BusRunner.rollback.
	require.NoError(t, manager.Delete(ctx, registry.Entry{ID: bindingID, Kind: apidi.Binding}))
	require.NoError(t, manager.Discard(ctx))

	assert.Nil(t, manager.tx)
	assert.Empty(t, manager.definitions)
	assert.Empty(t, manager.bindings)
}

// TestManager_TransactionEventsFlushOnCommitOnly: staged operations emit their
// contract-plane events at Commit and never before. Delivery order across two
// independent subscribers is not asserted; the bus dispatches concurrently.
func TestManager_TransactionEventsFlushOnCommitOnly(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, bus := setupDIManagerTest()
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")
	seedContract(t, manager, defID, bindingID, "method1")

	var mu sync.Mutex
	var kinds []string
	var wg sync.WaitGroup
	subscribe := func(kind string) *eventbus.Subscriber {
		sub, err := eventbus.NewSubscriber(ctx, bus, contract.System, kind, func(event.Event) {
			mu.Lock()
			kinds = append(kinds, kind)
			mu.Unlock()
			wg.Done()
		})
		require.NoError(t, err)
		return sub
	}
	defer subscribe(contract.UpdateDefinition).Close()
	defer subscribe(contract.UpdateBinding).Close()

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Update(ctx, definitionEntry(defID, "method1", "method2")))
	require.NoError(t, manager.Update(ctx, bindingEntry(bindingID, defID, "method1", "method2")))

	mu.Lock()
	assert.Empty(t, kinds, "no event may be emitted before Commit")
	mu.Unlock()

	wg.Add(2)
	require.NoError(t, manager.Commit(ctx))
	wg.Wait()

	mu.Lock()
	assert.ElementsMatch(t, []string{contract.UpdateDefinition, contract.UpdateBinding}, kinds)
	mu.Unlock()
}

// TestManager_TransactionDiscard drops every staged mutation and leaves the
// manager fully usable outside a transaction afterwards.
func TestManager_TransactionDiscard(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, _ := setupDIManagerTest()
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")
	seedContract(t, manager, defID, bindingID, "method1")

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Update(ctx, definitionEntry(defID, "method1", "method2")))
	require.NoError(t, manager.Discard(ctx))

	assert.Len(t, manager.definitions[defID].Methods, 1)

	// Non-transactional operation still behaves exactly as before.
	err := manager.Update(ctx, definitionEntry(defID, "method1", "method2"))
	requireAPIError(t, err, apierror.Invalid, "would invalidate")
}

// TestManager_TransactionDeletesDefinitionWithItsBindings: retiring a contract
// and its binding in one changeset commits; retiring the definition while a
// binding survives fails at Commit as an unresolved contract reference.
func TestManager_TransactionDeletesDefinitionWithItsBindings(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")

	t.Run("definition and binding together", func(t *testing.T) {
		manager, _ := setupDIManagerTest()
		seedContract(t, manager, defID, bindingID, "method1")

		require.NoError(t, manager.Begin(ctx))
		require.NoError(t, manager.Delete(ctx, registry.Entry{ID: defID, Kind: apidi.Definition}))
		require.NoError(t, manager.Delete(ctx, registry.Entry{ID: bindingID, Kind: apidi.Binding}))
		require.NoError(t, manager.Commit(ctx))

		assert.Empty(t, manager.definitions)
		assert.Empty(t, manager.bindings)
	})

	t.Run("definition alone leaves the binding unresolved", func(t *testing.T) {
		manager, _ := setupDIManagerTest()
		seedContract(t, manager, defID, bindingID, "method1")

		require.NoError(t, manager.Begin(ctx))
		require.NoError(t, manager.Delete(ctx, registry.Entry{ID: defID, Kind: apidi.Definition}))
		err := manager.Commit(ctx)
		requireAPIError(t, err, apierror.Invalid, "binding references undefined contract")

		assert.Contains(t, manager.definitions, defID)
		assert.Contains(t, manager.bindings, bindingID)
	})
}

// TestManager_TransactionCreateOrderIndependence: within a transaction a
// binding may arrive before the definition it references; Commit sees both.
func TestManager_TransactionCreateOrderIndependence(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, _ := setupDIManagerTest()
	defID := registry.NewID("test", "contract")
	bindingID := registry.NewID("test", "binding")

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Add(ctx, bindingEntry(bindingID, defID, "method1")))
	require.NoError(t, manager.Add(ctx, definitionEntry(defID, "method1")))
	require.NoError(t, manager.Commit(ctx))

	assert.Contains(t, manager.definitions, defID)
	assert.Contains(t, manager.bindings, bindingID)
}

// TestManager_TransactionDuplicateDefaultRejectedAtCommit: unique-default
// enforcement holds across staged and committed bindings alike.
func TestManager_TransactionDuplicateDefaultRejectedAtCommit(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, _ := setupDIManagerTest()
	defID := registry.NewID("test", "contract")

	defaultBinding := func(id registry.ID) registry.Entry {
		return registry.Entry{
			ID:   id,
			Kind: apidi.Binding,
			Data: NewMockPayload(&apidi.BindingConfig{
				Contracts: []apidi.BoundContractConfig{
					{
						Contract: defID.String(),
						Default:  true,
						Methods:  map[string]string{"method1": "app:impl.method1"},
					},
				},
			}),
		}
	}

	require.NoError(t, manager.Add(ctx, definitionEntry(defID, "method1")))
	require.NoError(t, manager.Add(ctx, defaultBinding(registry.NewID("test", "binding1"))))

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Add(ctx, defaultBinding(registry.NewID("test", "binding2"))))
	err := manager.Commit(ctx)
	requireAPIError(t, err, apierror.AlreadyExists, "default binding")

	assert.NotContains(t, manager.bindings, registry.NewID("test", "binding2"))
}

// TestManager_TransactionExistenceChecksSeeStagedState: staged mutations are
// visible to subsequent operations in the same transaction.
func TestManager_TransactionExistenceChecksSeeStagedState(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	manager, _ := setupDIManagerTest()
	defID := registry.NewID("test", "contract")

	require.NoError(t, manager.Begin(ctx))
	require.NoError(t, manager.Add(ctx, definitionEntry(defID, "method1")))

	err := manager.Add(ctx, definitionEntry(defID, "method1"))
	requireAPIError(t, err, apierror.AlreadyExists, "already exists")

	require.NoError(t, manager.Delete(ctx, registry.Entry{ID: defID, Kind: apidi.Definition}))
	err = manager.Update(ctx, definitionEntry(defID, "method1"))
	requireAPIError(t, err, apierror.NotFound, "not found for update")

	require.NoError(t, manager.Discard(ctx))
}
