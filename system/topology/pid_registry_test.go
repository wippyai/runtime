package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

func TestPIDRegistry_Register(t *testing.T) {
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	// Create test PIDs
	pid1 := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	pid2 := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq2",
	}

	// Test successful registration
	err := reg.Register("name1", pid1)
	assert.NoError(t, err)

	// Test overwriting existing registration (should be allowed for atomic pointer changes)
	err = reg.Register("name1", pid2)
	assert.NoError(t, err)

	// Verify the name now points to pid2
	resolvedPID, found := reg.Lookup("name1")
	assert.True(t, found)
	assert.Equal(t, pid2, resolvedPID)

	// Test successful registration of different name and Target
	err = reg.Register("name2", pid2)
	assert.NoError(t, err)

	// Test registering same Target with different name (should be allowed)
	err = reg.Register("name3", pid1)
	assert.NoError(t, err)
}
func TestPIDRegistry_Lookup(t *testing.T) {
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Register a name
	err := reg.Register("test-name", pid)
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

	pid := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Register a name
	err := reg.Register("test-name", pid)
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
	parentPID := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	childPID := pid.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq2",
	}

	// Register a name in parent
	err := parentReg.Register("parent-name", parentPID)
	assert.NoError(t, err)

	// Register a name in child
	err = childReg.Register("child-name", childPID)
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
	reg := NewPIDRegistry(WithLogger(zap.NewNop()))

	const numRoutines = 100

	// Create a bunch of unique PIDs and names
	pids := make([]pid.PID, numRoutines)
	names := make([]string, numRoutines)

	for i := 0; i < numRoutines; i++ {
		pids[i] = pid.PID{
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
			err := reg.Register(names[idx], pids[idx])
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

	pid := pid.PID{Node: "node1", Host: "host1", UniqID: "uniq1"}

	// Register multiple names for the same PID
	_ = reg.Register("name1", pid)
	_ = reg.Register("name2", pid)
	_ = reg.Register("name3", pid)

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

	pid := pid.PID{Node: "node1", Host: "host1", UniqID: "uniq1"}

	// Register in both
	_ = parent.Register("parent-name", pid)
	_ = child.Register("child-name", pid)

	// Remove from child should propagate to parent
	child.Remove(pid)

	// Both should be gone
	_, found := child.Lookup("child-name")
	assert.False(t, found)
	_, found = parent.Lookup("parent-name")
	assert.False(t, found)
}
