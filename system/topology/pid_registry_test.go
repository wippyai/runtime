// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"go.uber.org/zap"
)

// fakeGlobalRegistry satisfies topology.GlobalRegistry. active names resolve
// via Lookup; reserved names surface via IsStrongReserved (the promotion-window
// guard) but NOT via Lookup, mirroring the real service.
type fakeGlobalRegistry struct {
	active   map[string]pidapi.PID
	reserved map[string]pidapi.PID
	notReady bool
}

func (f *fakeGlobalRegistry) Lookup(_ context.Context, name string, _ ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	if p, ok := f.active[name]; ok {
		return globalapi.LookupResult{PID: p, Found: true}, nil
	}
	return globalapi.LookupResult{}, nil
}

func (f *fakeGlobalRegistry) IsStrongReserved(name string) (pidapi.PID, bool) {
	p, ok := f.reserved[name]
	return p, ok
}

func (f *fakeGlobalRegistry) NameReady() bool { return !f.notReady }

type fakeEventualRegistry struct {
	active map[string]pidapi.PID
}

func (f *fakeEventualRegistry) Lookup(_ context.Context, name string, _ ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	if p, ok := f.active[name]; ok {
		return globalapi.LookupResult{PID: p, Found: true}, nil
	}
	return globalapi.LookupResult{}, nil
}

// TestPIDRegistry_StrongReservationBlocksLocalBind proves a held Strong
// reservation refuses a LOCAL bind of the same name to a different pid, and
// allows the same pid, through the existing global-reg shadow-check seam.
func TestPIDRegistry_StrongReservationBlocksLocalBind(t *testing.T) {
	reserved := pidapi.PID{Node: "node-1", Host: "host", UniqID: "owner"}
	gr := &fakeGlobalRegistry{
		active:   map[string]pidapi.PID{},
		reserved: map[string]pidapi.PID{"system.root": reserved},
	}
	reg := NewPIDRegistry(WithLogger(zap.NewNop()), WithGlobalRegistry(gr))

	// A different pid is refused while the reservation is held.
	other := pidapi.PID{Node: "node-1", Host: "host", UniqID: "other"}
	existing, err := reg.Register("system.root", other)
	assert.ErrorIs(t, err, topology.ErrNameAlreadyRegistered)
	assert.Equal(t, reserved, existing, "the reserved pid is surfaced as taken")

	// The same (reserved) pid is allowed.
	got, err := reg.Register("system.root", reserved)
	assert.NoError(t, err)
	assert.Equal(t, reserved, got)

	// Once the reservation clears, a fresh LOCAL bind succeeds.
	delete(gr.reserved, "system.root")
	got, err = reg.Register("system.free", other)
	assert.NoError(t, err)
	assert.Equal(t, other, got)
}

// TestPIDRegistry_JoinBarrierGatesLocalRegister proves a fresh LOCAL register is
// refused with ErrNameServiceNotReady while the join-epoch barrier is in
// progress, and allowed once it completes. A re-register of a name this node
// already holds is allowed even while not ready (no shadowing risk).
func TestPIDRegistry_JoinBarrierGatesLocalRegister(t *testing.T) {
	gr := &fakeGlobalRegistry{
		active:   map[string]pidapi.PID{},
		reserved: map[string]pidapi.PID{},
		notReady: true,
	}
	reg := NewPIDRegistry(WithLogger(zap.NewNop()), WithGlobalRegistry(gr))

	p := pidapi.PID{Node: "node-1", Host: "host", UniqID: "p1"}
	_, err := reg.Register("local.gated", p)
	assert.ErrorIs(t, err, topology.ErrNameServiceNotReady, "fresh local register refused while barrier in progress")

	// Barrier completes — the same register now succeeds.
	gr.notReady = false
	got, err := reg.Register("local.gated", p)
	assert.NoError(t, err)
	assert.Equal(t, p, got)
}

// TestPIDRegistry_JoinBarrierAllowsReRegister proves a re-register of an
// already-held name to the same pid is allowed even while the barrier runs.
func TestPIDRegistry_JoinBarrierAllowsReRegister(t *testing.T) {
	gr := &fakeGlobalRegistry{active: map[string]pidapi.PID{}, reserved: map[string]pidapi.PID{}}
	reg := NewPIDRegistry(WithLogger(zap.NewNop()), WithGlobalRegistry(gr))

	p := pidapi.PID{Node: "node-1", Host: "host", UniqID: "p1"}
	_, err := reg.Register("local.held", p)
	require.NoError(t, err)

	// Barrier (re)opens — a re-register of the held name to the same pid is safe.
	gr.notReady = true
	got, err := reg.Register("local.held", p)
	assert.NoError(t, err)
	assert.Equal(t, p, got)
}

// TestPIDRegistry_LookupIncludesEventualRegistry is the regression for Lua
// process.registry.lookup resolving EVENTUAL names: Lua calls PIDRegistry.Lookup,
// so a name learned by the gossip registry must be visible here.
func TestPIDRegistry_LookupIncludesEventualRegistry(t *testing.T) {
	p := pidapi.PID{Node: "node-2", Host: "host", UniqID: "evt"}
	reg := NewPIDRegistry(WithLogger(zap.NewNop()), WithEventualRegistry(&fakeEventualRegistry{
		active: map[string]pidapi.PID{"session.remote": p},
	}))

	got, ok := reg.Lookup("session.remote")
	assert.True(t, ok)
	assert.Equal(t, p, got)
}

// TestPIDRegistry_LookupScopePrecedence documents the composed lookup order:
// global names win over EVENTUAL names, and EVENTUAL names win over LOCAL names.
func TestPIDRegistry_LookupScopePrecedence(t *testing.T) {
	localPID := pidapi.PID{Node: "node-1", Host: "host", UniqID: "local"}
	eventualPID := pidapi.PID{Node: "node-2", Host: "host", UniqID: "eventual"}
	globalPID := pidapi.PID{Node: "node-3", Host: "host", UniqID: "global"}

	reg := NewPIDRegistry(WithLogger(zap.NewNop()))
	_, err := reg.Register("svc.shared", localPID)
	require.NoError(t, err)

	gr := &fakeGlobalRegistry{
		active:   map[string]pidapi.PID{"svc.shared": globalPID},
		reserved: map[string]pidapi.PID{},
	}
	er := &fakeEventualRegistry{active: map[string]pidapi.PID{"svc.shared": eventualPID}}
	reg.SetGlobalRegistry(gr)
	reg.SetEventualRegistry(er)

	got, ok := reg.Lookup("svc.shared")
	require.True(t, ok)
	assert.Equal(t, globalPID, got)

	delete(gr.active, "svc.shared")
	got, ok = reg.Lookup("svc.shared")
	require.True(t, ok)
	assert.Equal(t, eventualPID, got)

	delete(er.active, "svc.shared")
	got, ok = reg.Lookup("svc.shared")
	require.True(t, ok)
	assert.Equal(t, localPID, got)
}

func TestPIDRegistry_RegisterRejectsLocalShadowingEventualName(t *testing.T) {
	eventualPID := pidapi.PID{Node: "node-2", Host: "host", UniqID: "eventual"}
	localPID := pidapi.PID{Node: "node-1", Host: "host", UniqID: "local"}
	reg := NewPIDRegistry(WithLogger(zap.NewNop()), WithEventualRegistry(&fakeEventualRegistry{
		active: map[string]pidapi.PID{"svc.shared": eventualPID},
	}))

	existing, err := reg.Register("svc.shared", localPID)
	assert.ErrorIs(t, err, topology.ErrNameAlreadyRegistered)
	assert.Equal(t, eventualPID, existing)
}

func TestPIDRegistry_Register(t *testing.T) {
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	// Create test PIDs
	pid1 := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	pid2 := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq2",
	}

	// Test successful registration - returns the registered PID
	existingPID, err := reg.Register("name1", pid1)
	assert.NoError(t, err)
	assert.Equal(t, pid1, existingPID)

	// Test re-registering same name with same PID (should be allowed)
	existingPID, err = reg.Register("name1", pid1)
	assert.NoError(t, err)
	assert.Equal(t, pid1, existingPID)

	// Test registering existing name with different PID (should fail and return existing)
	existingPID, err = reg.Register("name1", pid2)
	assert.ErrorIs(t, err, topology.ErrNameAlreadyRegistered)
	assert.Equal(t, pid1, existingPID) // Returns the existing PID

	// Verify the name still points to pid1
	resolvedPID, found := reg.Lookup("name1")
	assert.True(t, found)
	assert.Equal(t, pid1, resolvedPID)

	// Test successful registration of different name
	existingPID, err = reg.Register("name2", pid2)
	assert.NoError(t, err)
	assert.Equal(t, pid2, existingPID)

	// Test registering same PID with different name (should be allowed)
	existingPID, err = reg.Register("name3", pid1)
	assert.NoError(t, err)
	assert.Equal(t, pid1, existingPID)
}

func TestPIDRegistry_Lookup(t *testing.T) {
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Register a name
	_, err := reg.Register("test-name", pid)
	assert.NoError(t, err)

	// Test successful lookup
	resolvedPID, found := reg.Lookup("test-name")
	assert.True(t, found)
	assert.Equal(t, pid, resolvedPID)

	// Test looking up non-existent name
	_, found = reg.Lookup("non-existent")
	assert.False(t, found)
}

func TestPIDRegistry_Unregister(t *testing.T) {
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Register a name
	_, err := reg.Register("test-name", pid)
	assert.NoError(t, err)

	// Test successful unregister
	unreg := reg.Unregister("test-name")
	assert.True(t, unreg)

	// Verify name is no longer registered
	_, found := reg.Lookup("test-name")
	assert.False(t, found)

	// Test unregistering non-existent name
	unreg = reg.Unregister("non-existent")
	assert.False(t, unreg)
}

func TestPIDRegistry_WithParent(t *testing.T) {
	logger := zap.NewNop()

	// Create parent registry
	parentReg := NewPIDRegistry(WithLogger(logger))

	// Create child registry with parent
	childReg := NewPIDRegistry(WithParent(parentReg), WithLogger(logger))

	// Create test PIDs
	parentPID := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	childPID := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq2",
	}

	// Register a name in parent
	_, err := parentReg.Register("parent-name", parentPID)
	assert.NoError(t, err)

	// Register a name in child
	_, err = childReg.Register("child-name", childPID)
	assert.NoError(t, err)

	// Test child looking up its own registration
	resolvedPID, found := childReg.Lookup("child-name")
	assert.True(t, found)
	assert.Equal(t, childPID, resolvedPID)

	// Test child looking up parent's registration
	resolvedPID, found = childReg.Lookup("parent-name")
	assert.True(t, found)
	assert.Equal(t, parentPID, resolvedPID)

	// Test unregistering name from parent via child
	unregistered := childReg.Unregister("parent-name")
	assert.True(t, unregistered)

	// Verify parent name is unregistered
	_, found = parentReg.Lookup("parent-name")
	assert.False(t, found)
}

func TestPIDRegistry_ThreadSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping thread safety stress test in short mode")
	}

	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	const numRoutines = 100

	// Create a bunch of unique PIDs and names
	pids := make([]pidapi.PID, numRoutines)
	names := make([]string, numRoutines)

	for i := 0; i < numRoutines; i++ {
		pids[i] = pidapi.PID{
			Node:   "node1",
			Host:   "host1",
			UniqID: "uniq" + string(rune(i)),
		}
		names[i] = "name-" + string(rune(i))
	}

	// Use a channel for goroutine synchronization
	done := make(chan bool, numRoutines)

	// Spawn routines to register
	for i := 0; i < numRoutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()
			_, err := reg.Register(names[idx], pids[idx])
			if err != nil {
				t.Errorf("Failed to register: %v", err)
			}
		}(i)
	}

	// Wait for all registrations
	for i := 0; i < numRoutines; i++ {
		<-done
	}

	// Verify all registrations
	for i := 0; i < numRoutines; i++ {
		pid, found := reg.Lookup(names[i])
		assert.True(t, found)
		assert.Equal(t, pids[i], pid)
	}

	// Spawn routines to lookup and unregister
	for i := 0; i < numRoutines; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			// Lookup
			pid, found := reg.Lookup(names[idx])
			if !found || pid != pids[idx] {
				t.Errorf("Failed to look up name %s", names[idx])
			}

			// Unregister
			unregistered := reg.Unregister(names[idx])
			if !unregistered {
				t.Errorf("Failed to unregister name %s", names[idx])
			}
		}(i)
	}

	// Wait for all operations
	for i := 0; i < numRoutines; i++ {
		<-done
	}

	// Verify all names are unregistered
	for i := 0; i < numRoutines; i++ {
		_, found := reg.Lookup(names[i])
		assert.False(t, found)
	}
}

func TestPIDRegistry_Remove(t *testing.T) {
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	pid := pidapi.PID{Node: "node1", Host: "host1", UniqID: "uniq1"}

	// Register multiple names for the same PID
	_, _ = reg.Register("name1", pid)
	_, _ = reg.Register("name2", pid)
	_, _ = reg.Register("name3", pid)

	// Verify all names resolve
	_, found := reg.Lookup("name1")
	assert.True(t, found)
	_, found = reg.Lookup("name2")
	assert.True(t, found)
	_, found = reg.Lookup("name3")
	assert.True(t, found)

	// Remove the PID entirely
	reg.Remove(pid)

	// All names should be gone
	_, found = reg.Lookup("name1")
	assert.False(t, found)
	_, found = reg.Lookup("name2")
	assert.False(t, found)
	_, found = reg.Lookup("name3")
	assert.False(t, found)
}

func TestPIDRegistry_RemoveWithParent(t *testing.T) {
	parent := NewPIDRegistry(WithLogger(zap.NewNop()))
	child := NewPIDRegistry(WithParent(parent), WithLogger(zap.NewNop()))

	pid := pidapi.PID{Node: "node1", Host: "host1", UniqID: "uniq1"}

	// Register in both
	_, _ = parent.Register("parent-name", pid)
	_, _ = child.Register("child-name", pid)

	// Remove from child should propagate to parent
	child.Remove(pid)

	// Both should be gone
	_, found := child.Lookup("child-name")
	assert.False(t, found)
	_, found = parent.Lookup("parent-name")
	assert.False(t, found)
}

func TestPIDRegistry_RemoveNotFoundDelegatesToParent(t *testing.T) {
	parent := NewPIDRegistry(WithLogger(zap.NewNop()))
	child := NewPIDRegistry(WithParent(parent), WithLogger(zap.NewNop()))

	pid := pidapi.PID{Node: "node1", Host: "host1", UniqID: "uniq1"}

	// Register only in parent
	_, _ = parent.Register("parent-only", pid)

	// Remove from child - PID not in child, should delegate to parent
	child.Remove(pid)

	// Parent registration should be gone
	_, found := parent.Lookup("parent-only")
	assert.False(t, found)
}

func TestPIDRegistry_RemoveNonExistent(t *testing.T) {
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	p := pidapi.PID{Node: "node1", Host: "host1", UniqID: "nonexistent"}

	// Remove non-existent PID - should not panic
	reg.Remove(p)

	// Verify lookup still returns false
	_, found := reg.Lookup("nonexistent")
	assert.False(t, found)
}

func TestPIDRegistry_RemoveNonExistentWithParent(t *testing.T) {
	parent := NewPIDRegistry(WithLogger(zap.NewNop()))
	child := NewPIDRegistry(WithParent(parent), WithLogger(zap.NewNop()))

	p := pidapi.PID{Node: "node1", Host: "host1", UniqID: "nonexistent"}

	// Remove non-existent PID from child - should delegate to parent, not panic
	child.Remove(p)

	// Verify lookup still returns false
	_, found := child.Lookup("nonexistent")
	assert.False(t, found)
}
