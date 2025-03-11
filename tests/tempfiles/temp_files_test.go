package tempfiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTempDirWithFiles(t *testing.T) {
	files := map[string]string{
		"listener/listener.yaml": "listener content",
		"template/template.html": "template content",
		"main.yaml":              "main content",
		"sub/dir/file.txt":       "sub file content",
	}

	rootDir, cleanup := TempDirWithFiles(t, "test-dir", files)
	defer cleanup() // Ensure cleanup is done

	// Now, you can perform assertions on the created file structure and content

	configFile := filepath.Join(rootDir, "listener/listener.yaml")
	content, err := os.ReadFile(configFile)

	if err != nil {
		t.Fatalf("Failed to read file %v", configFile)
	}

	if string(content) != "listener content" {
		t.Fatalf("Unexpected file content")
	}

	templateFile := filepath.Join(rootDir, "template/template.html")
	templateContent, err := os.ReadFile(templateFile)

	if err != nil {
		t.Fatalf("Failed to read file %v", templateFile)
	}
	if string(templateContent) != "template content" {
		t.Fatalf("Unexpected file content")
	}

	mainFile := filepath.Join(rootDir, "main.yaml")
	mainContent, err := os.ReadFile(mainFile)
	if err != nil {
		t.Fatalf("Failed to read file %v", mainFile)
	}

	if string(mainContent) != "main content" {
		t.Fatalf("Unexpected file content")
	}

	subFile := filepath.Join(rootDir, "sub/dir/file.txt")
	subContent, err := os.ReadFile(subFile)

	if err != nil {
		t.Fatalf("Failed to read file %v", subFile)
	}

	if string(subContent) != "sub file content" {
		t.Fatalf("Unexpected file content")
	}
}
