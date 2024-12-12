package history

import (
	"github.com/ponyruntime/pony/internal/version"
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
)

func TestMemoryHistory_Record_SingleAction(t *testing.T) {
	h := NewMemory()

	action := registry.Action{
		Kind: registry.Create,
		Entry: registry.Entry{
			Path: "test/path",
			Kind: "test-kind",
			Meta: registry.Metadata{"key": "value"},
			Data: payload.NewString("test-data"),
		},
	}

	newVersion, err := h.Record(action)
	if err != nil {
		t.Fatalf("unexpected error recording action: %v", err)
	}

	// Check current version
	currentVersion, err := h.Current()
	if err != nil {
		t.Fatalf("unexpected error getting current version: %v", err)
	}
	if !currentVersion.Equals(newVersion) {
		t.Errorf("current version does not match recorded version\ngot:  %v\nwant: %v", currentVersion, newVersion)
	}

	// Check recorded actions for the new version
	recordedActions, err := h.GetActions(newVersion)
	if err != nil {
		t.Fatalf("unexpected error getting actions for version: %v", err)
	}
	if len(recordedActions) != 1 {
		t.Fatalf("expected 1 recorded action, got %d", len(recordedActions))
	}

	if !reflect.DeepEqual(recordedActions[0], action) {
		t.Errorf("recorded action does not match original action\ngot:  %v\nwant: %v", recordedActions[0], action)
	}
}

func TestMemoryHistory_Record_MultipleActions(t *testing.T) {
	h := NewMemory()

	actions := []registry.Action{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "path/one",
				Kind: "kind-one",
				Meta: registry.Metadata{"k1": "v1"},
			},
		},
		{
			Kind: registry.Update,
			Entry: registry.Entry{
				Path: "path/two",
				Kind: "kind-two",
				Meta: registry.Metadata{"k2": "v2"},
			},
		},
		{
			Kind: registry.Delete,
			Entry: registry.Entry{
				Path: "path/three",
				Kind: "kind-three",
			},
		},
	}

	newVersion, err := h.Record(actions...)
	if err != nil {
		t.Fatalf("unexpected error recording actions: %v", err)
	}

	recordedActions, err := h.GetActions(newVersion)
	if err != nil {
		t.Fatalf("unexpected error getting actions for version: %v", err)
	}
	if len(recordedActions) != 3 {
		t.Fatalf("expected 3 recorded actions, got %d", len(recordedActions))
	}

	if !reflect.DeepEqual(recordedActions, actions) {
		t.Errorf("recorded actions do not match original actions\ngot:  %v\nwant: %v", recordedActions, actions)
	}
}

func TestMemoryHistory_Versions(t *testing.T) {
	h := NewMemory()

	// Record some actions to create different versions
	v1, _ := h.Record(registry.Action{Kind: registry.Create, Entry: registry.Entry{Path: "path/one"}})
	v2, _ := h.Record(registry.Action{Kind: registry.Update, Entry: registry.Entry{Path: "path/two"}})
	v3, _ := h.Record(registry.Action{Kind: registry.Delete, Entry: registry.Entry{Path: "path/three"}})

	versions, err := h.Versions()
	if err != nil {
		t.Fatalf("unexpected error getting versions: %v", err)
	}

	expectedVersions := []registry.Version{
		version.New(1, 0), // Initial version
		v1,
		v2,
		v3,
	}

	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("returned versions do not match expected versions\ngot:  %v\nwant: %v", versions, expectedVersions)
	}
}

func TestMemoryHistory_Versions_Corrected(t *testing.T) {
	h := NewMemory()

	// Record some actions to create different versions
	v1, _ := h.Record(registry.Action{Kind: registry.Create, Entry: registry.Entry{Path: "path/one"}})
	v2, _ := h.Record(registry.Action{Kind: registry.Update, Entry: registry.Entry{Path: "path/two"}})
	v3, _ := h.Record(registry.Action{Kind: registry.Delete, Entry: registry.Entry{Path: "path/three"}})

	versions, err := h.Versions()
	if err != nil {
		t.Fatalf("unexpected error getting versions: %v", err)
	}

	expectedVersions := []registry.Version{
		version.New(1, 0), // Initial version
		v1,
		v2,
		v3,
	}

	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("returned versions do not match expected versions\ngot:  %v\nwant: %v", versions, expectedVersions)
	}
}

func TestMemoryHistory_Record_Conflict(t *testing.T) {
	h := NewMemory()

	actions := []registry.Action{
		{
			Kind: registry.Create,
			Entry: registry.Entry{
				Path: "path/one",
				Kind: "kind-one",
			},
		},
		{
			Kind: registry.Create, // Conflict: same path as the previous action
			Entry: registry.Entry{
				Path: "path/one",
				Kind: "kind-two",
			},
		},
	}

	_, err := h.Record(actions...)
	if err == nil {
		t.Fatalf("expected error recording conflicting actions, got nil")
		return
	}

	expectedErrorMsg := "conflict: multiple create actions for path 'path/one' in the same version"
	if err.Error() != expectedErrorMsg {
		t.Errorf("unexpected error message\ngot:  %s\nwant: %s", err.Error(), expectedErrorMsg)
	}
}

func TestMemoryHistory_GetActions_NonExistentVersion(t *testing.T) {
	h := NewMemory()

	_, err := h.GetActions(version.New(99, 99)) // Non-existent version
	if err == nil {
		t.Errorf("expected error getting actions for non-existent version, got nil")
		return
	}

	expectedErrorMsg := "version not found: v00099.099"
	if err.Error() != expectedErrorMsg {
		t.Errorf("unexpected error message\ngot:  %s\nwant: %s", err.Error(), expectedErrorMsg)
	}
}

func TestMemoryHistory_Seek(t *testing.T) {
	h := NewMemory()

	// Create some versions
	v1, _ := h.Record(registry.Action{Kind: registry.Create, Entry: registry.Entry{Path: "path/one"}})
	_, _ = h.Record(registry.Action{Kind: registry.Update, Entry: registry.Entry{Path: "path/two"}})
	v3, _ := h.Record(registry.Action{Kind: registry.Delete, Entry: registry.Entry{Path: "path/three"}})

	testCases := []struct {
		name          string
		seekTo        registry.Version
		expectedError string
	}{
		{
			name:          "seek to initial version",
			seekTo:        version.New(1, 0),
			expectedError: "",
		},
		{
			name:          "seek to v1",
			seekTo:        v1,
			expectedError: "",
		},
		{
			name:          "seek to v3",
			seekTo:        v3,
			expectedError: "",
		},
		{
			name:          "seek to non-existent version",
			seekTo:        version.New(99, 99),
			expectedError: "version not found: v00099.099",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.Seek(tc.seekTo)
			if tc.expectedError == "" {
				if err != nil {
					t.Fatalf("unexpected error seeking to version %v: %v", tc.seekTo, err)
				}

				// Verify current version after successful seek
				current, err := h.Current()
				if err != nil {
					t.Fatalf("unexpected error getting current version: %v", err)
				}
				if !current.Equals(tc.seekTo) {
					t.Errorf("current version is %v, expected %v", current, tc.seekTo)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error seeking to version %v, got nil", tc.seekTo)
				}
				if err.Error() != tc.expectedError {
					t.Errorf("unexpected error message\ngot:  %s\nwant: %s", err.Error(), tc.expectedError)
				}
			}
		})
	}
}
