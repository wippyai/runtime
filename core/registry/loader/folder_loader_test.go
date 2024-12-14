package loader

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	transcoder "github.com/ponyruntime/pony/core/payload"
	"github.com/ponyruntime/pony/core/payload/json"
	"github.com/ponyruntime/pony/core/payload/yaml"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"testing"
)

func TestFolderLoader_Load_MultipleFiles_Interpolation(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "folderloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	files := map[string]string{
		"a.yaml": `
name: setting_a
kind: config
data: value_a
`,
		"b/b.yaml": `
name: setting_b
kind: config
data: file://b_data.txt
`,
		"b/b_data.txt": "value_b",
		"c/nested/c.yaml": `
name: setting_c
kind: config
data: "interpolated_${from_a}"
`,
		"d/d.yaml": `
name: setting_d
kind: config
data: "interpolated_${not_exist}"
`,
	}
	for path, content := range files {
		fullPath := filepath.Join(tempDir, path)
		err = os.MkdirAll(filepath.Dir(fullPath), 0755) // Create directories if they don't exist
		if err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}

	// Create a transcoder and logger for testing
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Create FolderLoader with variables
	vars := Variables{
		"from_a": "value_from_a",
	}
	folderLoader := NewFolderLoader(dtt, logger)

	// Load the entries
	entries, err := folderLoader.Load(tempDir, vars)
	if err != nil {
		t.Fatalf("failed to load entries: %v", err)
	}

	// Assert that four entries were loaded
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Expected Entry setup
	expectedEntries := map[string]struct {
		Kind string
		Data map[string]interface{}
	}{
		"setting_a": {
			Kind: "config",
			Data: map[string]interface{}{
				"name": "setting_a",
				"kind": "config",
				"data": "value_a",
			},
		},
		"b.setting_b": {
			Kind: "config",
			Data: map[string]interface{}{
				"name": "setting_b",
				"kind": "config",
				"data": "value_b",
			},
		},
		"c.nested.setting_c": {
			Kind: "config",
			Data: map[string]interface{}{
				"name": "setting_c",
				"kind": "config",
				"data": "interpolated_value_from_a",
			},
		},
		"d.setting_d": {
			Kind: "config",
			Data: map[string]interface{}{
				"name": "setting_d",
				"kind": "config",
				"data": "interpolated_${not_exist}",
			},
		},
	}

	// Compare loaded entries
	for _, entry := range entries {
		expected, ok := expectedEntries[string(entry.Path)]
		if !ok {
			t.Fatalf("expected entry with path '%s' not found", entry.Path)
		}

		if entry.Kind != registry.Kind(expected.Kind) {
			t.Errorf("expected entry kind: %s, got: %s for path: %s", expected.Kind, entry.Kind, entry.Path)
		}
		var data map[string]interface{}
		err = dtt.Unmarshal(entry.Data, &data)
		if err != nil {
			t.Fatalf("failed to unmarshal payload data for path %s: %v", entry.Path, err)
		}
		assert.Equal(t, expected.Data, data, "Data mismatch for path: %s", entry.Path)
	}
}

func TestFolderLoader_Load_NoFiles(t *testing.T) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "folderloader_empty")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	folderLoader := NewFolderLoader(dtt, logger)

	vars := Variables{
		"from_a": "value_from_a",
	}
	// Load the entries from empty directory
	entries, err := folderLoader.Load(tempDir, vars)
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
	tempDir, err := os.MkdirTemp("", "folderloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	files := map[string]string{
		"a.txt": "unsupported content",
	}

	for path, content := range files {
		fullPath := filepath.Join(tempDir, path)
		err = os.MkdirAll(filepath.Dir(fullPath), 0755) // Create directories if they don't exist
		if err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}
	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	folderLoader := NewFolderLoader(dtt, logger)

	vars := Variables{
		"from_a": "value_from_a",
	}
	// Load the entries
	entries, err := folderLoader.Load(tempDir, vars)
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
	tempDir, err := os.MkdirTemp("", "folderloader_invalid_yaml")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	files := map[string]string{
		"a.yaml": "invalid yaml content",
	}
	for path, content := range files {
		fullPath := filepath.Join(tempDir, path)
		err = os.MkdirAll(filepath.Dir(fullPath), 0755) // Create directories if they don't exist
		if err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		err = os.WriteFile(fullPath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}

	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()

	folderLoader := NewFolderLoader(dtt, zap.NewNop())

	vars := Variables{
		"from_a": "value_from_a",
	}
	// Load the entries
	entries, err := folderLoader.Load(tempDir, vars)
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

	// Register JSON
	tr.RegisterTranscoder(payload.Json, payload.Golang, 1, &json.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.Json, 1, &json.FromGolang{})
	tr.RegisterUnmarshaler(payload.Json, &json.ToGolang{})

	// Register YAML
	tr.RegisterTranscoder(payload.Yaml, payload.Golang, 1, &yaml.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.Yaml, 1, &yaml.FromGolang{})
	tr.RegisterUnmarshaler(payload.Yaml, &yaml.ToGolang{})

	return tr
}
