package storage

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/version"
)

func TestMemoryStorage_Versions(t *testing.T) {
	storage := NewMemory()

	v1 := version.New(1)
	v2 := version.New(2)
	v3 := version.New(3)

	_ = storage.Save(v1, registry.OperationSet{})
	_ = storage.Save(v2, registry.OperationSet{})
	_ = storage.Save(v3, registry.OperationSet{})

	versions, err := storage.Versions()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// Sort the versions to ensure consistent comparison
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].ID() < versions[j].ID()
	})

	expectedVersions := []registry.Version{v1, v2, v3}
	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("Expected versions: %v, got: %v", expectedVersions, versions)
	}
}

func TestMemoryStorage_Get(t *testing.T) {
	storage := NewMemory()
	v2 := version.New(2)

	actions := registry.OperationSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	_ = storage.Save(v2, actions)

	retrievedActions, err := storage.Get(v2)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !reflect.DeepEqual(retrievedActions, actions) {
		t.Errorf("Expected actions: %v, got: %v", actions, retrievedActions)
	}

	_, err = storage.Get(version.New(3))
	if err == nil {
		t.Errorf("Expected error for non-existent version")
	}
}

func TestMemoryStorage_Save(t *testing.T) {
	storage := NewMemory()
	v1 := version.New(1)
	v2 := version.New(2)

	actions := registry.OperationSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	// Save v1 as well
	err := storage.Save(v1, registry.OperationSet{}) // Save v1 with an empty operation set
	if err != nil {
		t.Fatalf("Unexpected error saving v1: %v", err)
	}

	err = storage.Save(v2, actions)
	if err != nil {
		t.Fatalf("Unexpected error saving v2: %v", err)
	}

	versions, _ := storage.Versions()
	// Sort the versions to ensure consistent comparison
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].ID() < versions[j].ID()
	})

	expectedVersions := []registry.Version{v1, v2}
	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("Expected versions: %v, got: %v", expectedVersions, versions)
	}

	retrievedActions, _ := storage.Get(v2)
	if !reflect.DeepEqual(retrievedActions, actions) {
		t.Errorf("Expected actions: %v, got: %v", actions, retrievedActions)
	}
}

func TestMemoryStorage_Save_Conflict(t *testing.T) {
	storage := NewMemory()
	v2 := version.New(2)

	actions := registry.OperationSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "/foo",
				Kind: "test",
				Data: payload.New("data1"),
			},
		},
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "/foo",
				Kind: "test",
				Data: payload.New("data2"),
			},
		},
	}

	err := storage.Save(v2, actions)
	if err == nil {
		t.Errorf("Expected conflict error, got nil")
	}

	expectedError := fmt.Errorf("conflict: multiple create actions for path '/foo' in the same version")
	if err.Error() != expectedError.Error() {
		t.Errorf("Expected error: %v, got: %v", expectedError, err)
	}
}
