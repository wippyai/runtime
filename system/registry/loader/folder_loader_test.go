package loader

import (
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/utils"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestFolderLoader_Load_MultipleFiles_Interpolation(t *testing.T) {
	// Create a temporary directory structure for testing
	files := map[string]string{
		"a.yaml": `
name: setting_a
kind: listener
data: value_a
`,
		"b/b.yaml": `
name: setting_b
kind: listener
data: file://b_data.txt
`,
		"b/b_data.txt": "value_b",
		"c/nested/c.yaml": `
name: setting_c
kind: listener
data: "interpolated_${from_a}"
`,
		"d/d.yaml": `
name: setting_d
kind: listener
data: "interpolated_${not_exist}"
`,
		"e/e.yaml": `
name: setting_e
kind: listener
data: file://../e_data.txt
`,
		"e_data.txt": "value_e",
	}

	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_test", files)
	defer cleanup()

	// Create a transcoder and logger for testing
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer func() {
		_ = logger.Sync()
	}()

	// Create FolderLoader with variables
	vars := Variables{
		"from_a": "value_from_a",
	}
	folderLoader := NewFolderLoader(dtt, logger)

	// Load the entries
	entries, err := folderLoader.Load(rootDir, "", vars)
	if err != nil {
		t.Fatalf("failed to load entries: %v", err)
	}

	// Assert that four entries were loaded
	if len(entries) != 5 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Expected Entry setup
	expectedEntries := map[string]struct {
		Kind string
		Data map[string]interface{}
	}{
		"setting_a": {
			Kind: "listener",
			Data: map[string]interface{}{
				"name": "setting_a",
				"kind": "listener",
				"data": "value_a",
			},
		},
		"setting_b": {
			Kind: "listener",
			Data: map[string]interface{}{
				"name": "setting_b",
				"kind": "listener",
				"data": "value_b",
			},
		},
		"setting_c": {
			Kind: "listener",
			Data: map[string]interface{}{
				"name": "setting_c",
				"kind": "listener",
				"data": "interpolated_value_from_a",
			},
		},
		"setting_d": {
			Kind: "listener",
			Data: map[string]interface{}{
				"name": "setting_d",
				"kind": "listener",
				"data": "interpolated_${not_exist}",
			},
		},
		"setting_e": {
			Kind: "listener",
			Data: map[string]interface{}{
				"name": "setting_e",
				"kind": "listener",
				"data": "value_e",
			},
		},
	}

	// Compare loaded entries
	for _, entry := range entries {
		expected, ok := expectedEntries[string(entry.ID)]
		if !ok {
			t.Fatalf("expected entry with path '%s' not found", entry.ID)
		}

		if entry.Kind != registry.Kind(expected.Kind) {
			t.Errorf("expected entry kind: %s, got: %s for path: %s", expected.Kind, entry.Kind, entry.ID)
		}
		var data map[string]interface{}
		err = dtt.Unmarshal(entry.Data, &data)
		if err != nil {
			t.Fatalf("failed to unmarshal payload data for path %s: %v", entry.ID, err)
		}
		assert.Equal(t, expected.Data, data, "Data mismatch for path: %s", entry.ID)
	}
}

func TestFolderLoader_Load_NoFiles(t *testing.T) {
	// Create a temporary directory
	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_empty", nil)
	defer cleanup()

	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer func() {
		_ = logger.Sync()
	}()
	folderLoader := NewFolderLoader(dtt, logger)

	vars := Variables{
		"from_a": "value_from_a",
	}
	namespace := "test"
	// Load the entries from empty directory
	entries, err := folderLoader.Load(rootDir, namespace, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Check that the list is empty
	if len(entries) != 0 {
		t.Fatalf("expected empty entry list, got %d", len(entries))
	}
}

func TestFolderLoader_Load_UnsupportedFiles(t *testing.T) {
	// Create a temporary directory structure for testing
	files := map[string]string{
		"a.txt": "unsupported content",
	}

	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_test", files)
	defer cleanup()
	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer func() {
		_ = logger.Sync()
	}()

	folderLoader := NewFolderLoader(dtt, logger)

	vars := Variables{
		"from_a": "value_from_a",
	}
	namespace := "test"
	// Load the entries
	entries, err := folderLoader.Load(rootDir, namespace, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert that the entry list is empty
	if len(entries) != 0 {
		t.Fatalf("expected empty entry list, got %d", len(entries))
	}
}

func TestFolderLoader_Load_InvalidYaml(t *testing.T) {
	// Create a temporary directory structure for testing
	files := map[string]string{
		"a.yaml": "invalid yaml content",
	}
	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_invalid_yaml", files)
	defer cleanup()

	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()

	folderLoader := NewFolderLoader(dtt, zap.NewNop())
	namespace := "test"
	vars := Variables{
		"from_a": "value_from_a",
	}
	// Load the entries
	entries, err := folderLoader.Load(rootDir, namespace, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert that the entry list is empty
	if len(entries) != 0 {
		t.Fatalf("expected empty entry list, got %d", len(entries))
	}
}

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()
	json.Register(tr)
	yaml.Register(tr)

	return tr
}

func TestFolderLoader_Load_MissingNameOrKind(t *testing.T) {
	files := map[string]string{
		"no_name.yaml": `
kind: listener
data: value
`,
		"no_kind.yaml": `
name: setting
data: value
`,
		"valid.yaml": `
name: valid_setting
kind: listener
data: value
`,
	}

	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_missing", files)
	defer cleanup()

	dtt := createTestTranscoder()
	core, obs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	defer func() {
		_ = logger.Sync()
	}()

	folderLoader := NewFolderLoader(dtt, logger)
	vars := Variables{}
	entries, err := folderLoader.Load(rootDir, "", vars)
	if err != nil {
		t.Fatalf("failed to load entries: %v", err)
	}

	// Assert that only one valid entry was loaded
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if string(entries[0].ID) != "valid_setting" {
		t.Fatalf("expected entry with path '%s' not found", "valid_setting")
	}

	// Check logs for errors
	logged := obs.FilterMessage("failed to process entry, skipping")
	if len(logged.All()) != 2 {
		t.Fatalf("expected 2 error logs, got %d", len(logged.All()))
	}

	expectedMessages := []string{
		"failed to process entry, skipping",
		"failed to process entry, skipping",
	}
	for i, entry := range logged.All() {
		if !strings.Contains(entry.Message, expectedMessages[i]) {
			t.Errorf("expected error message '%s', got '%s'", expectedMessages[i], entry.Message)
		}
	}
}

func TestFolderLoader_Load_InvalidContent(t *testing.T) {
	files := map[string]string{
		"invalid.yaml": `
name: test_entry
kind: listener
data:
    invalid: [
    }
`,
		"valid.yaml": `
name: valid_setting
kind: listener
data: value
`,
	}
	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_invalid", files)
	defer cleanup()

	dtt := createTestTranscoder()
	core, obs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	defer func() {
		_ = logger.Sync()
	}()

	folderLoader := NewFolderLoader(dtt, logger)
	vars := Variables{}
	entries, err := folderLoader.Load(rootDir, "", vars)
	if err != nil {
		t.Fatalf("failed to load entries: %v", err)
	}

	// Assert that only the valid entry was loaded
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if string(entries[0].ID) != "valid_setting" {
		t.Fatalf("expected entry with path '%s' not found", "valid_setting")
	}
	// Check logs for errors
	logged := obs.FilterMessage("failed to process entry, skipping")
	if len(logged.All()) != 1 {
		t.Fatalf("expected 1 error log, got %d", len(logged.All()))
	}

	expectedMessage := "failed to process entry, skipping"
	if !strings.Contains(logged.All()[0].Message, expectedMessage) {
		t.Errorf("expected error message to contain '%s', got '%s'", expectedMessage, logged.All()[0].Message)
	}
}
