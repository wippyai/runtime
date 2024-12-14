package runner

import (
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

func TestStateHelper(t *testing.T) {
	sh := newStateHelper(zap.NewNop())

	// Initial state for testing
	initialState := registry.State{
		{
			Path: "service/api/host",
			Kind: "config",
			Data: payload.NewString("localhost"),
		},
		{
			Path: "service/api/port",
			Kind: "config",
			Data: payload.NewString("8080"),
		},
	}

	t.Run("toMapAndToSlice", func(t *testing.T) {
		stateMap := sh.toMap(initialState)
		if len(stateMap) != len(initialState) {
			t.Errorf("toMap() failed: expected map length %d, got %d", len(initialState), len(stateMap))
		}

		newState := sh.toSlice(stateMap)
		if len(newState) != len(initialState) {
			t.Errorf("toSlice() failed: expected slice length %d, got %d", len(initialState), len(newState))
		}

		// Basic check to ensure data integrity
		for _, entry := range initialState {
			if _, ok := stateMap[entry.Path]; !ok {
				t.Errorf("toSlice() failed: entry with path %s missing in map", entry.Path)
			}
		}
	})

	t.Run("copy", func(t *testing.T) {
		originalMap := sh.toMap(initialState)
		copiedMap := sh.copy(originalMap)

		if len(copiedMap) != len(originalMap) {
			t.Errorf("copy() failed: expected map length %d, got %d", len(originalMap), len(copiedMap))
		}

		// Modify copied map and ensure original map remains unchanged
		delete(copiedMap, "service/api/host")
		if _, ok := originalMap["service/api/host"]; !ok {
			t.Errorf("copy() failed: original map was modified")
		}
	})

	t.Run("applyChangeToState", func(t *testing.T) {
		stateMap := sh.toMap(initialState)

		// Test Create operation
		newEntry := registry.Entry{Path: "service/db/host", Kind: "config", Data: payload.NewString("db.local")}
		createOp := registry.Operation{Kind: registry.Create, Entry: newEntry}
		newStateMap, err := sh.applyChangeToState(stateMap, createOp)
		if err != nil {
			t.Errorf("applyChangeToState() failed for Create: %v", err)
		}
		if _, ok := newStateMap["service/db/host"]; !ok {
			t.Errorf("applyChangeToState() failed: Create operation did not add new entry")
		}

		// Test Update operation
		updateOp := registry.Operation{Kind: registry.Update, Entry: registry.Entry{Path: "service/api/host", Kind: "config", Data: payload.NewString("api.local")}}
		newStateMap, err = sh.applyChangeToState(newStateMap, updateOp)
		if err != nil {
			t.Errorf("applyChangeToState() failed for Update: %v", err)
		}
		if entry, ok := newStateMap["service/api/host"]; !ok || entry.Data.Data() != "api.local" {
			t.Errorf("applyChangeToState() failed: Update operation did not update entry")
		}

		// Test Delete operation
		deleteOp := registry.Operation{Kind: registry.Delete, Entry: registry.Entry{Path: "service/api/port"}}
		newStateMap, err = sh.applyChangeToState(newStateMap, deleteOp)
		if err != nil {
			t.Errorf("applyChangeToState() failed for Delete: %v", err)
		}
		if _, ok := newStateMap["service/api/port"]; ok {
			t.Errorf("applyChangeToState() failed: Delete operation did not delete entry")
		}

		// Test Delete non-existing entry
		deleteOpNonExist := registry.Operation{Kind: registry.Delete, Entry: registry.Entry{Path: "non/existent/path"}}
		newStateMap, err = sh.applyChangeToState(newStateMap, deleteOpNonExist)
		if err != nil {
			t.Errorf("applyChangeToState() failed for Delete non-exist: %v", err)
		}

		// Test unknown operation
		unknownOp := registry.Operation{Kind: "unknown", Entry: newEntry}
		_, err = sh.applyChangeToState(newStateMap, unknownOp)
		if err == nil {
			t.Errorf("applyChangeToState() failed: expected error for unknown operation kind")
		}
	})

	t.Run("getInverseOperation", func(t *testing.T) {
		stateMap := sh.toMap(initialState)

		// Test Create inverse (Delete)
		createOp := registry.Operation{Kind: registry.Create, Entry: registry.Entry{Path: "service/new/path", Kind: "config", Data: payload.NewString("new_value")}}
		inverseOp, err := sh.getInverseOperation(stateMap, createOp)
		if err != nil {
			t.Errorf("getInverseOperation() failed for Create: %v", err)
		}
		if inverseOp.Kind != registry.Delete || inverseOp.Entry.Path != "service/new/path" {
			t.Errorf("getInverseOperation() failed: incorrect inverse for Create")
		}

		// Test Update inverse (Update with original entry)
		updateOp := registry.Operation{Kind: registry.Update, Entry: registry.Entry{Path: "service/api/host", Kind: "config", Data: payload.NewString("updated_value")}}
		inverseOp, err = sh.getInverseOperation(stateMap, updateOp)
		if err != nil {
			t.Errorf("getInverseOperation() failed for Update: %v", err)
		}
		if inverseOp.Kind != registry.Update || inverseOp.Entry.Path != "service/api/host" || inverseOp.Entry.Data.Data() != "localhost" {
			t.Errorf("getInverseOperation() failed: incorrect inverse for Update")
		}

		// Test Delete inverse (Create with original entry)
		deleteOp := registry.Operation{Kind: registry.Delete, Entry: registry.Entry{Path: "service/api/port"}}
		inverseOp, err = sh.getInverseOperation(stateMap, deleteOp)
		if err != nil {
			t.Errorf("getInverseOperation() failed for Delete: %v", err)
		}
		if inverseOp.Kind != registry.Create || inverseOp.Entry.Path != "service/api/port" || inverseOp.Entry.Data.Data() != "8080" {
			t.Errorf("getInverseOperation() failed: incorrect inverse for Delete")
		}

		// Test Update inverse for non-existing entry
		updateOpNotExist := registry.Operation{Kind: registry.Update, Entry: registry.Entry{Path: "non/existent/path", Kind: "config", Data: payload.NewString("invalid")}}
		_, err = sh.getInverseOperation(stateMap, updateOpNotExist)
		if err == nil {
			t.Errorf("getInverseOperation() failed: expected error for Update with non-existing original entry")
		}

		// Test Delete inverse for non-existing entry
		deleteOpNotExist := registry.Operation{Kind: registry.Delete, Entry: registry.Entry{Path: "non/existent/path"}}
		_, err = sh.getInverseOperation(stateMap, deleteOpNotExist)
		if err == nil {
			t.Errorf("getInverseOperation() failed: expected error for Delete with non-existing original entry")
		}
	})
}
