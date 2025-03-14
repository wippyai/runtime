package topology

import (
	"testing"

	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPIDRegistry_Register(t *testing.T) {
	logger := zap.NewNop()
	reg := NewPIDRegistry(PIDRegistryConfig{
		Logger: logger,
	})

	// Create test PIDs
	pid1 := pubsub.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ParseID("test:proc1"),
		UniqID: "uniq1",
	}

	pid2 := pubsub.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ParseID("test:proc2"),
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

	// Test successful registration of different name and PID
	err = reg.Register("name2", pid2)
	assert.NoError(t, err)

	// Test registering same PID with different name (should be allowed)
	err = reg.Register("name3", pid1)
	assert.NoError(t, err)
}
func TestPIDRegistry_Lookup(t *testing.T) {
	logger := zap.NewNop()
	reg := NewPIDRegistry(PIDRegistryConfig{
		Logger: logger,
	})

	pid := pubsub.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ParseID("test:proc"),
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
	logger := zap.NewNop()
	reg := NewPIDRegistry(PIDRegistryConfig{
		Logger: logger,
	})

	pid := pubsub.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ParseID("test:proc"),
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
	parentReg := NewPIDRegistry(PIDRegistryConfig{
		Logger: logger,
	})

	// Create child registry with parent
	childReg := NewPIDRegistry(PIDRegistryConfig{
		Parent: parentReg,
		Logger: logger,
	})

	// Create test PIDs
	parentPID := pubsub.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ParseID("test:parent"),
		UniqID: "uniq1",
	}

	childPID := pubsub.PID{
		Node:   "node1",
		Host:   "host1",
		ID:     registry.ParseID("test:child"),
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
	logger := zap.NewNop()
	reg := NewPIDRegistry(PIDRegistryConfig{
		Logger: logger,
	})

	const numRoutines = 100

	// Create a bunch of unique PIDs and names
	pids := make([]pubsub.PID, numRoutines)
	names := make([]string, numRoutines)

	for i := 0; i < numRoutines; i++ {
		pids[i] = pubsub.PID{
			Node:   "node1",
			Host:   "host1",
			ID:     registry.ParseID("test:proc" + string(rune(i))),
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
