// SPDX-License-Identifier: MPL-2.0

package process_test

import (
	"context"
	"sync"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	globalregapi "github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/globalreg"
)

// mockGlobalRegistry implements globalreg.Registry interface using FSM for testing.
type mockGlobalRegistry struct {
	fsm      *globalreg.FSM
	mu       sync.RWMutex
	logIndex uint64 // monotonically increasing Raft log index
}

func newMockGlobalRegistry() *mockGlobalRegistry {
	return &mockGlobalRegistry{
		fsm: globalreg.NewFSM(),
	}
}

func (m *mockGlobalRegistry) applyCommand(cmd *globalreg.Command) any {
	data, err := globalreg.EncodeCommand(cmd)
	if err != nil {
		return err
	}
	m.logIndex++
	return m.fsm.Apply(&hraft.Log{Data: data, Index: m.logIndex})
}

func (m *mockGlobalRegistry) Register(ctx context.Context, name string, p pid.PID) (pid.PID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := &globalreg.Command{
		Type:   globalreg.CmdRegister,
		Name:   name,
		PID:    p,
		NodeID: p.Node,
	}
	resp := m.applyCommand(cmd)
	result, ok := resp.(*globalreg.RegisterResult)
	if !ok {
		return pid.PID{}, resp.(error)
	}
	if result.Err != nil {
		return result.ExistingPID, result.Err
	}
	return result.PID, nil
}

func (m *mockGlobalRegistry) RegisterScope(ctx context.Context, name string, p pid.PID, _ globalregapi.RegistrationMode) (globalregapi.RegisterOutcome, error) {
	pidOut, err := m.Register(ctx, name, p)
	return globalregapi.RegisterOutcome{PID: pidOut, State: globalregapi.RegisterStateActive}, err
}

func (m *mockGlobalRegistry) UnregisterScope(ctx context.Context, name string, _ globalregapi.RegistrationMode) (bool, error) {
	return m.Unregister(ctx, name)
}

func (m *mockGlobalRegistry) Unregister(ctx context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := &globalreg.Command{Type: globalreg.CmdUnregister, Name: name}
	resp := m.applyCommand(cmd)
	result, ok := resp.(*globalreg.UnregisterResult)
	if !ok {
		return false, resp.(error)
	}
	return result.Removed, nil
}

func (m *mockGlobalRegistry) Lookup(_ context.Context, name string, opts ...globalregapi.LookupOption) (globalregapi.LookupResult, error) {
	var o globalregapi.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	state := m.fsm.State()
	if o.ByPID != nil {
		names := state.LookupByPID(*o.ByPID)
		return globalregapi.LookupResult{
			PID:         *o.ByPID,
			NamesForPID: names,
			Found:       len(names) > 0,
		}, nil
	}
	if o.WithFence {
		p, token, found := state.LookupWithFence(name)
		return globalregapi.LookupResult{PID: p, FenceToken: token, Found: found}, nil
	}
	p, found := state.Lookup(name)
	return globalregapi.LookupResult{PID: p, Found: found}, nil
}

func (m *mockGlobalRegistry) LookupWithFence(name string) globalregapi.LookupResult {
	r, _ := m.Lookup(context.Background(), name, globalregapi.WithFence())
	return r
}

func (m *mockGlobalRegistry) ValidateFence(name string, token uint64) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.fsm.State().ValidateFence(name, token) {
		return globalregapi.ErrStaleFence
	}
	return nil
}

func (m *mockGlobalRegistry) LookupByPID(p pid.PID) []string {
	r, _ := m.Lookup(context.Background(), "", globalregapi.ByPID(p))
	return r.NamesForPID
}

func (m *mockGlobalRegistry) Remove(ctx context.Context, p pid.PID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := &globalreg.Command{Type: globalreg.CmdRemovePID, PID: p}
	m.applyCommand(cmd)
	return nil
}

func (m *mockGlobalRegistry) RemoveNode(ctx context.Context, nodeID pid.NodeID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := &globalreg.Command{Type: globalreg.CmdRemoveNode, NodeID: nodeID}
	m.applyCommand(cmd)
	return nil
}

var _ globalregapi.Registry = (*mockGlobalRegistry)(nil)

func simpleLookup(reg globalregapi.Registry, name string) (pid.PID, bool) {
	res, err := reg.Lookup(context.Background(), name)
	if err != nil {
		return pid.PID{}, false
	}
	return res.PID, res.Found
}

// TestGlobalRegistry_Registration tests that the mock registry works correctly.
func TestGlobalRegistry_Registration(t *testing.T) {
	reg := newMockGlobalRegistry()
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register
	result, err := reg.Register(ctx, "my-service", p)
	require.NoError(t, err)
	assert.Equal(t, p, result)

	// Lookup
	found, ok := simpleLookup(reg, "my-service")
	assert.True(t, ok)
	assert.Equal(t, p, found)
}

// TestGlobalRegistry_Conflict tests duplicate registration fails.
func TestGlobalRegistry_Conflict(t *testing.T) {
	reg := newMockGlobalRegistry()
	ctx := context.Background()

	p1 := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}
	p2 := pid.PID{Host: "test", UniqID: "proc2", Node: "node1"}

	// First registration succeeds
	_, err := reg.Register(ctx, "shared", p1)
	require.NoError(t, err)

	// Second registration fails
	existing, err := reg.Register(ctx, "shared", p2)
	require.Error(t, err)
	assert.Equal(t, p1, existing)
}

// TestGlobalRegistry_Concurrent tests concurrent registrations.
func TestGlobalRegistry_Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	reg := newMockGlobalRegistry()
	ctx := context.Background()

	numGoroutines := 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := pid.PID{Host: "test", UniqID: string(rune('a' + idx)), Node: "node1"}
			_, err := reg.Register(ctx, "concurrent-test", p)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// All but one should have failed
	errorCount := 0
	for range errors {
		errorCount++
	}
	assert.Equal(t, numGoroutines-1, errorCount)
}

// TestGlobalRegistry_MultipleNames tests registering multiple names.
func TestGlobalRegistry_MultipleNames(t *testing.T) {
	reg := newMockGlobalRegistry()
	ctx := context.Background()

	names := []string{"svc1", "svc2", "svc3", "svc4", "svc5"}

	for i, name := range names {
		p := pid.PID{Host: "test", UniqID: string(rune('a' + i)), Node: "node1"}
		_, err := reg.Register(ctx, name, p)
		require.NoError(t, err)
	}

	// Verify all exist
	for _, name := range names {
		_, ok := simpleLookup(reg, name)
		assert.True(t, ok, "name %s should exist", name)
	}
}

// TestGlobalRegistry_Unregister tests unregistration.
func TestGlobalRegistry_Unregister(t *testing.T) {
	reg := newMockGlobalRegistry()
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register
	_, err := reg.Register(ctx, "temp", p)
	require.NoError(t, err)

	// Verify exists
	_, ok := simpleLookup(reg, "temp")
	assert.True(t, ok)

	// Unregister
	removed, err := reg.Unregister(ctx, "temp")
	require.NoError(t, err)
	assert.True(t, removed)

	// Verify gone
	_, ok = simpleLookup(reg, "temp")
	assert.False(t, ok)
}

// TestGlobalRegistry_RemoveNode tests node cleanup.
func TestGlobalRegistry_RemoveNode(t *testing.T) {
	reg := newMockGlobalRegistry()
	ctx := context.Background()

	// Register from node1
	p1 := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}
	p2 := pid.PID{Host: "test", UniqID: "proc2", Node: "node1"}
	p3 := pid.PID{Host: "test", UniqID: "proc3", Node: "node2"}

	_, _ = reg.Register(ctx, "svc1", p1)
	_, _ = reg.Register(ctx, "svc2", p2)
	_, _ = reg.Register(ctx, "svc3", p3)

	// Remove node1
	err := reg.RemoveNode(ctx, "node1")
	require.NoError(t, err)

	// svc1 and svc2 should be gone
	_, ok := simpleLookup(reg, "svc1")
	assert.False(t, ok)
	_, ok = simpleLookup(reg, "svc2")
	assert.False(t, ok)

	// svc3 should remain
	_, ok = simpleLookup(reg, "svc3")
	assert.True(t, ok)
}

// TestGlobalRegistry_Linearizability tests that operations are ordered.
func TestGlobalRegistry_Linearizability(t *testing.T) {
	reg := newMockGlobalRegistry()
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// Register
	_, err := reg.Register(ctx, "linear", p)
	require.NoError(t, err)

	// Immediately lookup - should find (linearizability)
	found, ok := simpleLookup(reg, "linear")
	assert.True(t, ok)
	assert.Equal(t, p, found)
}

// TestGlobalRegistry_IdempotentReRegistration tests same PID re-registration.
func TestGlobalRegistry_IdempotentReRegistration(t *testing.T) {
	reg := newMockGlobalRegistry()
	ctx := context.Background()

	p := pid.PID{Host: "test", UniqID: "proc1", Node: "node1"}

	// First registration
	_, err := reg.Register(ctx, "idempotent", p)
	require.NoError(t, err)

	// Re-registration with same PID should succeed (idempotent)
	result, err := reg.Register(ctx, "idempotent", p)
	require.NoError(t, err)
	assert.Equal(t, p, result)
}

// TestGlobalRegistry_Stress tests high load.
func TestGlobalRegistry_Stress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	reg := newMockGlobalRegistry()
	ctx := context.Background()

	numOperations := 100

	// Rapid register/unregister cycles
	for i := 0; i < numOperations; i++ {
		name := "stress-" + string(rune('a'+i%26))
		p := pid.PID{Host: "test", UniqID: string(rune('a' + i%26)), Node: "node1"}

		_, err := reg.Register(ctx, name, p)
		require.NoError(t, err)

		_, _ = reg.Unregister(ctx, name)
	}

	// Final state should be empty (all unregistered)
	for i := 0; i < numOperations; i++ {
		name := "stress-" + string(rune('a'+i%26))
		_, ok := simpleLookup(reg, name)
		assert.False(t, ok, "name %s should not exist", name)
	}
}
