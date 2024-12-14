package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"go.uber.org/zap"
)

func TestEntryLoader_Load_SingleFile(t *testing.T) {
	// Create a temporary directory with a single JSON file
	tempDir, err := os.MkdirTemp("", "entryloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFilePath := filepath.Join(tempDir, "data.json")
	testFileContent := `{"key": "value"}`
	err = os.WriteFile(testFilePath, []byte(testFileContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Load the file using EntryLoader
	loader := NewEntryLoader(zap.NewNop()) // Use a no-op logger for tests
	payloads, err := loader.Load(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert that one payload was loaded
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	// Assert the payload's format, content, and path
	expectedPath := "data" // No extension, dot-separated
	p, ok := payloads[expectedPath]
	if !ok {
		t.Fatalf("expected payload with path '%s' not found", expectedPath)
	}
	if p.Format() != payload.Json {
		t.Errorf("expected format: %s, got: %s", payload.Json, p.Format())
	}
	if string(p.Data().([]byte)) != testFileContent {
		t.Errorf("expected data: %s, got: %s", testFileContent, string(p.Data().([]byte)))
	}
}

func TestEntryLoader_Load_MultipleFiles(t *testing.T) {
	// Create a temporary directory with multiple files (JSON and YAML)
	tempDir, err := os.MkdirTemp("", "entryloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	files := map[string]string{
		"data1.json": `{"key": "json_value"}`,
		"data2.yaml": "key: yaml_value",
	}
	for name, content := range files {
		err = os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0644)
		if err != nil {
			t.Fatalf("failed to write test file %s: %v", name, err)
		}
	}

	// Load the files using EntryLoader
	loader := NewEntryLoader(zap.NewNop()) // Use a no-op logger for tests
	payloads, err := loader.Load(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert that two payloads were loaded
	if len(payloads) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(payloads))
	}

	// Assert the payloads' formats, contents, and paths
	expectedPayloads := map[string]struct {
		Format  payload.Format
		Content string
	}{
		"data1": {Format: payload.Json, Content: files["data1.json"]},
		"data2": {Format: payload.Yaml, Content: files["data2.yaml"]},
	}
	for path, expected := range expectedPayloads {
		p, ok := payloads[path]
		if !ok {
			t.Errorf("expected payload with path '%s' not found", path)
			continue
		}
		if p.Format() != expected.Format {
			t.Errorf("expected format: %s, got: %s for path: %s", expected.Format, p.Format(), path)
		}
		if string(p.Data().([]byte)) != expected.Content {
			t.Errorf("expected data: %s, got: %s for path: %s", expected.Content, string(p.Data().([]byte)), path)
		}
	}
}

func TestEntryLoader_Load_UnsupportedFileType(t *testing.T) {
	// Create a temporary directory with an unsupported file type
	tempDir, err := os.MkdirTemp("", "entryloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFilePath := filepath.Join(tempDir, "data.txt")
	err = os.WriteFile(testFilePath, []byte("unsupported content"), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Load the files using EntryLoader
	loader := NewEntryLoader(zap.NewNop()) // Use a no-op logger for tests
	payloads, err := loader.Load(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert that no payloads were loaded
	if len(payloads) != 0 {
		t.Fatalf("expected 0 payloads, got %d", len(payloads))
	}
}

func TestEntryLoader_Load_EmptyDirectory(t *testing.T) {
	// Create an empty temporary directory
	tempDir, err := os.MkdirTemp("", "entryloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Load the files using EntryLoader
	loader := NewEntryLoader(zap.NewNop())
	payloads, err := loader.Load(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert that no payloads were loaded
	if len(payloads) != 0 {
		t.Fatalf("expected 0 payloads, got %d", len(payloads))
	}
}

func TestEntryLoader_Load_NestedDirectories(t *testing.T) {
	// Create a temporary directory with nested directories and files
	tempDir, err := os.MkdirTemp("", "entryloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create nested directories
	nestedDir := filepath.Join(tempDir, "nested")
	err = os.Mkdir(nestedDir, 0755)
	if err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	// Create files in the root and nested directories
	files := map[string]string{
		filepath.Join(tempDir, "root.json"):     `{"key": "root_value"}`,
		filepath.Join(nestedDir, "nested.yaml"): "key: nested_value",
	}
	for path, content := range files {
		err = os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatalf("failed to write test file %s: %v", path, err)
		}
	}

	// Load the files using EntryLoader
	loader := NewEntryLoader(zap.NewNop())
	payloads, err := loader.Load(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert that two payloads were loaded
	if len(payloads) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(payloads))
	}

	// Assert the payloads' formats, contents, and paths
	expectedPayloads := map[string]struct {
		Format  payload.Format
		Content string
	}{
		"root":          {Format: payload.Json, Content: files[filepath.Join(tempDir, "root.json")]},
		"nested/nested": {Format: payload.Yaml, Content: files[filepath.Join(nestedDir, "nested.yaml")]},
	}

	for path, expected := range expectedPayloads {
		p, ok := payloads[path]
		if !ok {
			t.Errorf("expected payload with path '%s' not found", path)
			continue
		}
		if p.Format() != expected.Format {
			t.Errorf("expected format: %s, got: %s for path: %s", expected.Format, p.Format(), path)
		}
		if string(p.Data().([]byte)) != expected.Content {
			t.Errorf("expected data: %s, got: %s for path: %s", expected.Content, string(p.Data().([]byte)), path)
		}
	}
}

func TestEntryLoader_loadFileAsPayload_UnsupportedFormat(t *testing.T) {
	loader := NewEntryLoader(zap.NewNop())

	// Create a dummy file (it won't be read in this case)
	tempDir, err := os.MkdirTemp("", "entryloader_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	dummyFilePath := filepath.Join(tempDir, "dummy.txt")
	_ = os.WriteFile(dummyFilePath, []byte("dummy content"), 0644)

	_, err = loader.loadFileAsPayload(dummyFilePath, "unsupported") // Pass a path, but an unsupported format
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != "unsupported file format: unsupported" {
		t.Errorf("unexpected error message: %v", err)
	}
}
