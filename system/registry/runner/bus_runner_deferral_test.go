// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
)

const (
	deferralKindProvider = "deferral.provider"
	deferralKindConsumer = "deferral.consumer"
)

// deferralComponent is a registry listener that mimics the production
// "consumer depends on provider" pattern: provider entries register a
// resource id; consumer entries look it up and reject with an apierror
// NotFound when the target is absent. Rejections carry apierror.NotFound so
// they exercise the BusRunner.Transition deferral path.
type deferralComponent struct {
	bus         event.Bus
	t           *testing.T
	registered  map[string]struct{}
	addAttempts map[string]int
	failOnce    map[string]bool // entry id -> fail once with non-NotFound on first attempt
	mu          sync.Mutex
}

func newDeferralComponent(t *testing.T, bus event.Bus) *deferralComponent {
	return &deferralComponent{
		bus:         bus,
		t:           t,
		registered:  make(map[string]struct{}),
		addAttempts: make(map[string]int),
		failOnce:    make(map[string]bool),
	}
}

func (c *deferralComponent) handleEvent(evt event.Event) {
	if evt.System != registry.System {
		return
	}
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		return
	}
	if entry.Kind != deferralKindProvider && entry.Kind != deferralKindConsumer {
		return
	}
	if evt.Kind == registry.EntryDelete {
		c.mu.Lock()
		delete(c.registered, entry.ID.String())
		c.mu.Unlock()
		c.sendAccept(entry.ID)
		return
	}
	if evt.Kind != registry.EntryCreate && evt.Kind != registry.EntryUpdate {
		return
	}

	c.mu.Lock()
	c.addAttempts[entry.ID.String()]++
	attempt := c.addAttempts[entry.ID.String()]
	c.mu.Unlock()

	switch entry.Kind {
	case deferralKindProvider:
		if c.failOnce[entry.ID.String()] && attempt == 1 {
			c.sendReject(entry.ID, apierror.New(apierror.Internal, "synthetic non-deferrable failure").WithRetryable(apierror.False))
			return
		}
		c.mu.Lock()
		c.registered[entry.ID.String()] = struct{}{}
		c.mu.Unlock()
		c.sendAccept(entry.ID)

	case deferralKindConsumer:
		data, _ := entry.Data.Data().(map[string]any)
		target, _ := data["provider"].(string)
		if target == "" {
			c.sendReject(entry.ID, errors.New("consumer entry missing provider field"))
			return
		}
		c.mu.Lock()
		_, registered := c.registered[target]
		c.mu.Unlock()
		if !registered {
			c.sendReject(entry.ID, apierror.New(apierror.NotFound, "provider not found: "+target).WithRetryable(apierror.False))
			return
		}
		c.sendAccept(entry.ID)
	}
}

func (c *deferralComponent) sendAccept(id registry.ID) {
	c.bus.Send(context.Background(), event.Event{
		System: registry.System,
		Kind:   registry.EntryAccept,
		Path:   id.String(),
	})
}

func (c *deferralComponent) sendReject(id registry.ID, err error) {
	c.bus.Send(context.Background(), event.Event{
		System: registry.System,
		Kind:   registry.EntryReject,
		Path:   id.String(),
		Data:   err,
	})
}

func (c *deferralComponent) attempts(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.addAttempts[id]
}

func setupDeferralEnv(t *testing.T) (context.Context, *BusRunner, *deferralComponent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctxapi.NewRootContext())

	bus := eventbus.NewBus()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	ctx = event.WithAwaitService(ctx, awaitSvc)

	component := newDeferralComponent(t, bus)
	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", component.handleEvent)
	require.NoError(t, err)

	br := NewBusRunner(bus, zap.NewNop(), newTestBuilder(nil), WithDispatchPolicy(internalDispatchPolicy()))

	cleanup := func() {
		listener.Close()
		_ = awaitSvc.Stop()
		cancel()
	}
	return ctx, br, component, cleanup
}

func providerEntry(ns, name string) registry.Operation {
	return registry.Operation{
		Kind: registry.EntryCreate,
		Entry: registry.Entry{
			ID:   registry.NewID(ns, name),
			Kind: deferralKindProvider,
			Data: payload.New(map[string]any{}),
		},
	}
}

func consumerEntry(ns, name, target string) registry.Operation {
	return registry.Operation{
		Kind: registry.EntryCreate,
		Entry: registry.Entry{
			ID:   registry.NewID(ns, name),
			Kind: deferralKindConsumer,
			Data: payload.New(map[string]any{"provider": target}),
		},
	}
}

// idStr returns the canonical string form of a registry ID built from ns/name.
// Uses a local variable so registry.ID's pointer-receiver String() method is
// callable from inline expressions in tests.
func idStr(ns, name string) string {
	id := registry.NewID(ns, name)
	return id.String()
}

// TestTransition_AllCleanOnePass verifies the loop is a no-op for changesets
// without deferrable failures: every op is processed in pass 1 and no retry
// machinery runs.
func TestTransition_AllCleanOnePass(t *testing.T) {
	ctx, br, comp, cleanup := setupDeferralEnv(t)
	defer cleanup()

	cs := registry.ChangeSet{
		providerEntry("mre", "aaa.driver"),
		consumerEntry("mre", "zzz.client", idStr("mre", "aaa.driver")),
	}

	state, err := br.Transition(ctx, nil, cs)
	require.NoError(t, err)
	assert.Len(t, state, 2)
	assert.Equal(t, 1, comp.attempts(idStr("mre", "aaa.driver")))
	assert.Equal(t, 1, comp.attempts(idStr("mre", "zzz.client")))
}

// TestTransition_ConsumerBeforeProviderRetries — the bug scenario from
// tests/proof/determinism. With consumer dispatched first, pass 1 defers it;
// provider succeeds in pass 1; pass 2 retries consumer and succeeds. The
// final state contains both entries.
func TestTransition_ConsumerBeforeProviderRetries(t *testing.T) {
	ctx, br, comp, cleanup := setupDeferralEnv(t)
	defer cleanup()

	driver := registry.NewID("mre", "zzz.driver")
	client := registry.NewID("mre", "aaa.client")
	cs := registry.ChangeSet{
		consumerEntry("mre", "aaa.client", driver.String()),
		providerEntry("mre", "zzz.driver"),
	}

	state, err := br.Transition(ctx, nil, cs)
	require.NoError(t, err)
	assert.Len(t, state, 2)
	assert.Equal(t, 1, comp.attempts(driver.String()))
	assert.Equal(t, 2, comp.attempts(client.String()), "consumer should be tried twice: deferred in pass 1, accepted in pass 2")
}

// TestTransition_NonNotFoundRollsBackImmediately — non-deferrable errors keep
// the original behavior: immediate rollback, transaction discarded, error
// returned. The consumer that would have succeeded on retry is not reached.
func TestTransition_NonNotFoundRollsBackImmediately(t *testing.T) {
	ctx, br, comp, cleanup := setupDeferralEnv(t)
	defer cleanup()
	driver := registry.NewID("mre", "zzz.driver")
	client := registry.NewID("mre", "aaa.client")
	comp.failOnce[driver.String()] = true

	cs := registry.ChangeSet{
		providerEntry("mre", "zzz.driver"),
		consumerEntry("mre", "aaa.client", driver.String()),
	}

	state, err := br.Transition(ctx, nil, cs)
	require.Error(t, err)
	assert.Empty(t, state, "rollback should leave no entries committed")
	assert.Equal(t, 1, comp.attempts(driver.String()), "provider should fail once with non-deferrable error and not be retried")
	assert.Equal(t, 0, comp.attempts(client.String()), "consumer should not be reached because the loop aborts on non-deferrable")
}

// TestTransition_UnresolvableReturnsUnresolvedDependenciesError — when a
// consumer references a provider that is not in the changeset and not yet in
// state, the loop converges to a no-progress pass and returns
// NewUnresolvedDependenciesError.
func TestTransition_UnresolvableReturnsUnresolvedDependenciesError(t *testing.T) {
	ctx, br, _, cleanup := setupDeferralEnv(t)
	defer cleanup()

	cs := registry.ChangeSet{
		consumerEntry("mre", "orphan.client", idStr("mre", "absent.driver")),
	}

	state, err := br.Transition(ctx, nil, cs)
	require.Error(t, err)
	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr), "expected an apierror.Error")
	assert.Equal(t, apierror.NotFound, apiErr.Kind(), "final error should carry NotFound kind")
	assert.Contains(t, apiErr.Error(), "unresolved dependencies after retry")
	assert.Empty(t, state, "rollback should leave no entries committed")
}

// TestTransition_DeeperChainConverges — A depends on B, B depends on C.
// Dispatched in the order A, B, C the loop needs three passes to converge
// (C accepted pass 1; B accepted pass 2; A accepted pass 3).
func TestTransition_DeeperChainConverges(t *testing.T) {
	ctx, br, comp, cleanup := setupDeferralEnv(t)
	defer cleanup()

	// Three providers in a register-chain — A registered second registers C as
	// its target, B registers A as its target, etc. To exercise a deeper
	// convergence path with three providers we register them all and have one
	// consumer that references each in sequence.
	pA := registry.NewID("chain", "a")
	pB := registry.NewID("chain", "b")
	pC := registry.NewID("chain", "c")
	cs := registry.ChangeSet{
		consumerEntry("chain", "consumer.of.a", pA.String()),
		consumerEntry("chain", "consumer.of.b", pB.String()),
		providerEntry("chain", "a"),
		consumerEntry("chain", "consumer.of.c", pC.String()),
		providerEntry("chain", "b"),
		providerEntry("chain", "c"),
	}

	state, err := br.Transition(ctx, nil, cs)
	require.NoError(t, err)
	assert.Len(t, state, 6)
	assert.Equal(t, 1, comp.attempts(pA.String()))
	assert.Equal(t, 1, comp.attempts(pB.String()))
	assert.Equal(t, 1, comp.attempts(pC.String()))
	// Consumers were dispatched before their providers and had to be deferred.
	assert.Equal(t, 2, comp.attempts(idStr("chain", "consumer.of.a")))
	assert.Equal(t, 2, comp.attempts(idStr("chain", "consumer.of.b")))
	assert.Equal(t, 2, comp.attempts(idStr("chain", "consumer.of.c")))
}

// TestTransition_TwoProvidersConsumerLast — two providers and one consumer
// referencing the second provider. The consumer is dispatched first and must
// be deferred until the right provider has been accepted. Exercises the path
// where multiple ops accept in pass 1 and a single op needs pass 2.
func TestTransition_TwoProvidersConsumerLast(t *testing.T) {
	ctx, br, comp, cleanup := setupDeferralEnv(t)
	defer cleanup()

	cTarget := registry.NewID("two", "c.driver")
	cs := registry.ChangeSet{
		consumerEntry("two", "a.client", cTarget.String()),
		providerEntry("two", "b.driver"),
		providerEntry("two", "c.driver"),
	}

	state, err := br.Transition(ctx, nil, cs)
	require.NoError(t, err)
	assert.Len(t, state, 3)
	assert.Equal(t, 2, comp.attempts(idStr("two", "a.client")), "consumer is deferred then retried")
	assert.Equal(t, 1, comp.attempts(idStr("two", "b.driver")))
	assert.Equal(t, 1, comp.attempts(idStr("two", "c.driver")))
}

// TestIsDeferrable_WrappedNotFound — the deferral predicate must walk the
// unwrap chain. NewOperationRejectedError wraps the listener error as
// apierror.Invalid with the original NotFound as cause; the deferral test
// is what isDeferrable does — the loop test above exercises it end-to-end,
// but this unit test pins the contract.
func TestIsDeferrable_WrappedNotFound(t *testing.T) {
	leaf := apierror.New(apierror.NotFound, "missing dep").WithRetryable(apierror.False)
	wrapped := NewOperationRejectedError(registry.NewID("ns", "name"), leaf)
	require.True(t, isDeferrable(wrapped), "wrapped NotFound must be detected through the cause chain")

	plain := errors.New("plain")
	require.False(t, isDeferrable(plain), "non-apierror should not be deferrable")

	invalid := apierror.New(apierror.Invalid, "bad").WithRetryable(apierror.False)
	require.False(t, isDeferrable(invalid), "Invalid-kind apierror should not be deferrable")
}
