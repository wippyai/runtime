package loader

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/utils"
	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/ponyruntime/pony/pkg/payload/yaml"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"strings"
	"testing"
)

func TestFolderLoader_Load_MultipleFiles_Interpolation(t *testing.T) {
	// Create a temporary directory structure for testing
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
		"e/e.yaml": `
name: setting_e
kind: config
data: file://../e_data.txt
`,
		"e_data.txt": "value_e",
	}

	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_test", files)
	defer cleanup()

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
		"e.setting_e": {
			Kind: "config",
			Data: map[string]interface{}{
				"name": "setting_e",
				"kind": "config",
				"data": "value_e",
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
	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_empty", nil)
	defer cleanup()

	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer logger.Sync()
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
	defer logger.Sync()

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

func TestFolderLoader_calculateFullID(t *testing.T) {
	testCases := []struct {
		name      string
		relPath   string
		entryName string
		namespace string
		expected  string
	}{
		{
			name:      "root level",
			relPath:   "",
			entryName: "test_entry",
			namespace: "test",
			expected:  "test:test_entry",
		},
		{
			name:      "single level",
			relPath:   "folder/",
			entryName: "test_entry",
			namespace: "test",
			expected:  "test:folder.test_entry",
		},
		{
			name:      "single level without /",
			relPath:   "folder",
			entryName: "test_entry",
			namespace: "test",
			expected:  "test:folder.test_entry",
		},
		{
			name:      "nested level",
			relPath:   "folder/nested/",
			entryName: "test_entry",
			namespace: "test",
			expected:  "test:folder.nested.test_entry",
		},
		{
			name:      "multiple slashes",
			relPath:   "folder//nested//",
			entryName: "test_entry",
			namespace: "test",
			expected:  "test:folder.nested.test_entry",
		},
		{
			name:      "root level with no namespace",
			relPath:   "",
			entryName: "test_entry",
			namespace: "",
			expected:  "test_entry",
		},
		{
			name:      "single level with no namespace",
			relPath:   "folder/",
			entryName: "test_entry",
			namespace: "",
			expected:  "folder.test_entry",
		},
	}

	// Initialize FolderLoader, transcoder, and logger
	dtt := createTestTranscoder()
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	folderLoader := NewFolderLoader(dtt, logger)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			folderLoader.namespace = tc.namespace
			result := folderLoader.calculateFullID(tc.relPath, tc.entryName)
			assert.Equal(t, registry.Path(tc.expected), result, "Expected full ID does not match")
		})
	}
}
func TestFolderLoader_Load_MissingNameOrKind(t *testing.T) {
	files := map[string]string{
		"no_name.yaml": `
kind: config
data: value
`,
		"no_kind.yaml": `
name: setting
data: value
`,
		"valid.yaml": `
name: valid_setting
kind: config
data: value
`,
	}

	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_missing", files)
	defer cleanup()

	dtt := createTestTranscoder()
	core, obs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	defer logger.Sync()

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

	if string(entries[0].Path) != "valid_setting" {
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
kind: config
data:
    invalid: [
    }
`,
		"valid.yaml": `
name: valid_setting
kind: config
data: value
`,
	}
	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_invalid", files)
	defer cleanup()

	dtt := createTestTranscoder()
	core, obs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	defer logger.Sync()

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

	if string(entries[0].Path) != "valid_setting" {
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

func TestFolderLoader_Load_DeeplyNestedFiles(t *testing.T) {
	files := map[string]string{
		"a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/file.yaml": `
name: nested_setting
kind: config
data: value
`,
	}

	rootDir, cleanup := utils.TempDirWithFiles(t, "folderloader_nested", files)
	defer cleanup()

	dtt := createTestTranscoder()
	logger := zap.NewNop() // use nop logger, no need to observe logs
	folderLoader := NewFolderLoader(dtt, logger)
	vars := Variables{}
	entries, err := folderLoader.Load(rootDir, "", vars)

	if err != nil {
		t.Fatalf("failed to load entries: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	expectedPath := "a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p.q.r.s.t.nested_setting"
	if string(entries[0].Path) != expectedPath {
		t.Fatalf("expected entry with path '%s' not foundm, got %+v", expectedPath, entries)
	}
}
