package registry

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/tests/temp_files"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
)

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()

	// Register JSON
	tr.RegisterTranscoder(payload.JSON, payload.Golang, 1, &json.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.JSON, 1, &json.FromGolang{})
	tr.RegisterUnmarshaler(payload.JSON, &json.ToGolang{})

	// Register YAML
	tr.RegisterTranscoder(payload.YAML, payload.Golang, 1, &yaml.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.YAML, 1, &yaml.FromGolang{})
	tr.RegisterUnmarshaler(payload.YAML, &yaml.ToGolang{})

	return tr
}

// Integration test for initializing registry state from a folder
func TestInMemoryRegistry_InitFromFolder(t *testing.T) {
	// 1. Setup: Create a temporary directory with test files using the helper
	files := map[string]string{
		"listener/database.yaml": `
name: database_url
kind: listener
data:
  host: localhost
  port: 5432
`,
		"service/api.yaml": `
name: api_service
kind: service
data:
  url: http://localhost:8080
`,
	}
	rootDir, cleanup := temp_files.TempDirWithFiles(t, "registry_init_test", files)
	defer cleanup()

	// 2. Initialize components:
	//    - History (using history.Memory)
	//    - Runner (with logic to apply loaded entries)
	//    - StateBuilder
	//    - Loader
	//    - Registry
	history := history.NewMemory()
	runner := &CustomizableMockRunner{}
	stateBuilder := NewStateBuilder(zap.NewNop())
	dtt := createTestTranscoder()
	folderLoader := loader.NewFolderLoader(dtt, zap.NewNop())

	reg := NewRegistry(history, runner, stateBuilder, zap.NewNop()).(*reg)

	// 3. Load entries from the folder:
	entries, err := folderLoader.Load(rootDir, "", loader.Variables{})
	if err != nil {
		t.Fatalf("failed to load entries from folder: %v", err)
	}

	// 4. Mock Runner to apply loaded entries to the state:
	runner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		newState := state
		for _, change := range changes {
			switch change.Kind {
			case registry.Create:
				newState = append(newState, change.Entry)
			case registry.Update:
				found := false
				for i, entry := range newState {
					if entry.ID == change.Entry.ID {
						newState[i] = change.Entry
						found = true
						break
					}
				}
				if !found {
					return state, fmt.Errorf("entry not found for update: %s", change.Entry.ID)
				}
			case registry.Delete:
				for i, entry := range newState {
					if entry.ID == change.Entry.ID {
						newState = append(newState[:i], newState[i+1:]...)
						break
					}
				}
			default:
				return state, fmt.Errorf("unsupported operation kind: %s", change.Kind)
			}
		}
		return newState, nil
	}

	// 5. Apply the loaded entries as the initial ChangeSet:
	//    - Use loader.CreateChangeSetFromEntries to create a ChangeSet
	//    - Use reg.Apply() to initialize the state
	initialChangeSet := loader.CreateChangeSetFromEntries(entries)

	newVersion, err := reg.Apply(context.Background(), initialChangeSet)
	if err != nil {
		t.Fatalf("failed to apply initial ChangeSet: %v", err)
	}

	// 6. Verify:
	//    - The current version is v1
	//    - The state matches the expected state derived from the files
	if newVersion.ID() != 1 {
		t.Errorf("Expected current version to be 1, got: %v", newVersion.ID())
	}

	expectedState := registry.State{
		{
			ID:   "database_url",
			Kind: "listener",
			Data: payload.New(map[string]interface{}{
				"name": "database_url",
				"kind": "listener",
				"data": map[string]interface{}{
					"host": "localhost",
					"port": float64(5432), // YAML numbers are unmarshaled as float64
				},
			}),
		},
		{
			ID:   "api_service",
			Kind: "service",
			Data: payload.New(map[string]interface{}{
				"name": "api_service",
				"kind": "service",
				"data": map[string]interface{}{
					"url": "http://localhost:8080",
				},
			}),
		},
	}

	currentState, err := reg.GetAllEntries()
	if err != nil {
		t.Fatalf("failed to get all entries: %v", err)
	}

	if len(currentState) != len(expectedState) {
		t.Fatalf("Expected state length %d, got %d", len(expectedState), len(currentState))
	}

	for _, expectedEntry := range expectedState {
		found := false
		for _, currentEntry := range currentState {
			if currentEntry.ID == expectedEntry.ID {
				found = true
				assert.Equal(t, expectedEntry.Kind, currentEntry.Kind, "Kind mismatch for path: %s", expectedEntry.ID)

				// Compare Data field using assert.Equal for deep comparison of maps
				var expectedData, currentData map[string]interface{}
				err = dtt.Unmarshal(expectedEntry.Data, &expectedData)
				assert.NoError(t, err, "Error unmarshalling expected data")
				err = dtt.Unmarshal(currentEntry.Data, &currentData)
				assert.NoError(t, err, "Error unmarshalling current data")

				assert.Equal(t, expectedData, currentData, "Data mismatch for path: %s", expectedEntry.ID)
				break
			}
		}
		if !found {
			t.Errorf("Expected entry not found in state: %s", expectedEntry.ID)
		}
	}
}
