package nil

import (
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
)

func TestHistory_SaveAndHead(t *testing.T) {
	hist := New()

	// Initially, Head should return error
	_, err := hist.Head()
	if err == nil {
		t.Error("Expected error when Head is not set, got nil")
	}

	// Create versions
	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	// Save v0 as head
	err = hist.Save(v0, registry.ChangeSet{}, true)
	if err != nil {
		t.Errorf("Save should not return error, got: %v", err)
	}

	// Head should now return v0
	head, err := hist.Head()
	if err != nil {
		t.Errorf("Unexpected error from Head: %v", err)
	}
	if head.ID() != v0.ID() {
		t.Errorf("Expected head version %d, got %d", v0.ID(), head.ID())
	}

	// Save v1 as head
	err = hist.Save(v1, registry.ChangeSet{}, true)
	if err != nil {
		t.Errorf("Save should not return error, got: %v", err)
	}

	// Head should now return v1
	head, err = hist.Head()
	if err != nil {
		t.Errorf("Unexpected error from Head: %v", err)
	}
	if head.ID() != v1.ID() {
		t.Errorf("Expected head version %d, got %d", v1.ID(), head.ID())
	}
}

func TestHistory_SaveWithoutHead(t *testing.T) {
	hist := New()

	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	// Save v0 as head
	err := hist.Save(v0, registry.ChangeSet{}, true)
	if err != nil {
		t.Errorf("Save should not return error, got: %v", err)
	}

	// Save v1 without setting as head
	err = hist.Save(v1, registry.ChangeSet{}, false)
	if err != nil {
		t.Errorf("Save should not return error, got: %v", err)
	}

	// Head should still be v0
	head, err := hist.Head()
	if err != nil {
		t.Errorf("Unexpected error from Head: %v", err)
	}
	if head.ID() != v0.ID() {
		t.Errorf("Expected head to remain %d, got %d", v0.ID(), head.ID())
	}
}

func TestHistory_Get(t *testing.T) {
	hist := New()

	v0 := version.New(registry.RootVersion)
	changes := registry.ChangeSet{
		{Kind: registry.Create, Entry: registry.Entry{ID: registry.NewID("", "/test")}},
	}

	// Save a version with changes
	_ = hist.Save(v0, changes, true)

	// Attempt to Get the version - should return error
	_, err := hist.Get(v0)
	if err == nil {
		t.Fatal("Get should return error for nil History, got nil")
	}
	if err.Error() != "version history not available: registry configured with history disabled (enable_history=false)" {
		t.Errorf("Expected 'version history not available: registry configured with history disabled (enable_history=false)' error, got: %v", err)
	}
}

func TestHistory_Versions(t *testing.T) {
	hist := New()

	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	// Save multiple versions
	_ = hist.Save(v0, registry.ChangeSet{}, true)
	_ = hist.Save(v1, registry.ChangeSet{}, true)

	// Attempt to get Versions - should return error
	_, err := hist.Versions()
	if err == nil {
		t.Fatal("Versions should return error for nil History, got nil")
	}
	if err.Error() != "version history not available: registry configured with history disabled (enable_history=false)" {
		t.Errorf("Expected 'version history not available: registry configured with history disabled (enable_history=false)' error, got: %v", err)
	}
}

func TestHistory_SetHead(t *testing.T) {
	hist := New()

	v0 := version.New(registry.RootVersion)
	v1 := version.FromParent(v0, 1)

	// Save v1 as head
	_ = hist.Save(v1, registry.ChangeSet{}, true)

	// Attempt to rewind by setting head to v0 - should return error
	err := hist.SetHead(v0)
	if err == nil {
		t.Fatal("SetHead should return error for nil History, got nil")
	}
	if err.Error() != "version rollback not supported: registry configured with history disabled (enable_history=false)" {
		t.Errorf("Expected 'version rollback not supported: registry configured with history disabled (enable_history=false)' error, got: %v", err)
	}

	// Head should still be v1
	head, err := hist.Head()
	if err != nil {
		t.Errorf("Unexpected error from Head: %v", err)
	}
	if head.ID() != v1.ID() {
		t.Errorf("Expected head to remain %d, got %d", v1.ID(), head.ID())
	}
}

func TestHistory_ConcurrentSave(t *testing.T) {
	hist := New()

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrently save different versions
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			v := version.New(uint(routineID)) //nolint:gosec // test iteration
			_ = hist.Save(v, registry.ChangeSet{}, true)
		}(i)
	}

	wg.Wait()

	// Should be able to read head without panicking
	head, err := hist.Head()
	if err != nil {
		t.Errorf("Unexpected error from Head after concurrent saves: %v", err)
	}
	if head == nil {
		t.Error("Head should not be nil after concurrent saves")
	}
}

func TestHistory_ConcurrentHeadRead(t *testing.T) {
	hist := New()

	v0 := version.New(registry.RootVersion)
	_ = hist.Save(v0, registry.ChangeSet{}, true)

	var wg sync.WaitGroup
	numGoroutines := 20

	// Concurrently read head
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			head, err := hist.Head()
			if err != nil {
				t.Errorf("Unexpected error from concurrent Head read: %v", err)
			}
			if head.ID() != v0.ID() {
				t.Errorf("Expected head version %d, got %d", v0.ID(), head.ID())
			}
		}()
	}

	wg.Wait()
}
