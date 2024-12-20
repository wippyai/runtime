package history

import (
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

	_ = storage.Save(v1, registry.ChangeSet{}, false)
	_ = storage.Save(v2, registry.ChangeSet{}, false)
	_ = storage.Save(v3, registry.ChangeSet{}, false)

	versions, err := storage.Versions()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

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

	actions := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	_ = storage.Save(v2, actions, false)

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

	actions := registry.ChangeSet{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				ID:   "/foo",
				Kind: "test",
				Data: payload.New("data"),
			},
		},
	}

	err := storage.Save(v1, registry.ChangeSet{}, false)
	if err != nil {
		t.Fatalf("Unexpected error saving v1: %v", err)
	}

	err = storage.Save(v2, actions, false)
	if err != nil {
		t.Fatalf("Unexpected error saving v2: %v", err)
	}

	versions, _ := storage.Versions()
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

func TestMemoryStorage_Head(t *testing.T) {
	storage := NewMemory()

	_, err := storage.Head()
	if err == nil {
		t.Errorf("Expected error when getting head of empty history, got nil")
	}

	v1 := version.New(1)
	_ = storage.Save(v1, registry.ChangeSet{}, true)

	head, err := storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if !reflect.DeepEqual(head, v1) {
		t.Errorf("Expected head to be v1 (%v), got: %v", v1, head)
	}

	v2 := version.New(2)
	_ = storage.Save(v2, registry.ChangeSet{}, false)

	head, err = storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if !reflect.DeepEqual(head, v1) {
		t.Errorf("Expected head to remain v1 (%v), got: %v", v1, head)
	}

	v3 := version.New(3)
	_ = storage.Save(v3, registry.ChangeSet{}, true)

	head, err = storage.Head()
	if err != nil {
		t.Fatalf("Unexpected error getting head: %v", err)
	}
	if !reflect.DeepEqual(head, v3) {
		t.Errorf("Expected head to be v3 (%v), got: %v", v3, head)
	}
}
