package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type migration struct {
	apiPath     string
	servicePath string
	oldPkg      string
	newPkg      string
}

func main() {
	// Define all migrations
	migrations := []migration{
		{"api/service/aws/s3/errors.go", "service/aws/s3/errors.go", "github.com/wippyai/runtime/api/service/aws/s3", "github.com/wippyai/runtime/service/aws/s3"},
		{"api/service/di/errors.go", "service/di/errors.go", "github.com/wippyai/runtime/service/di", "github.com/wippyai/runtime/service/di"},
		{"api/service/exec/errors.go", "service/exec/errors.go", "github.com/wippyai/runtime/service/exec", "github.com/wippyai/runtime/service/exec"},
		{"api/service/fs/directory/errors.go", "service/fs/directory/errors.go", "github.com/wippyai/runtime/service/fs/directory", "github.com/wippyai/runtime/service/fs/directory"},
		{"api/service/host/errors.go", "service/host/errors.go", "github.com/wippyai/runtime/service/host", "github.com/wippyai/runtime/service/host"},
		{"api/service/security/policy/errors.go", "service/security/policy/errors.go", "github.com/wippyai/runtime/service/security/policy", "github.com/wippyai/runtime/service/security/policy"},
		{"api/service/security/tokenstore/errors.go", "service/security/tokenstore/errors.go", "github.com/wippyai/runtime/service/security/tokenstore", "github.com/wippyai/runtime/service/security/tokenstore"},
		{"api/service/store/memory/errors.go", "service/store/memory/errors.go", "github.com/wippyai/runtime/service/store/memory", "github.com/wippyai/runtime/service/store/memory"},
		{"api/service/store/sql/errors.go", "service/store/sql/errors.go", "github.com/wippyai/runtime/service/store/sql", "github.com/wippyai/runtime/service/store/sql"},
		{"api/service/template/errors.go", "service/template/errors.go", "github.com/wippyai/runtime/service/template", "github.com/wippyai/runtime/service/template"},
		{"api/service/temporal/errors.go", "service/temporal/errors.go", "github.com/wippyai/runtime/service/temporal", "github.com/wippyai/runtime/service/temporal"},
		{"api/service/terminal/errors.go", "service/terminal/errors.go", "github.com/wippyai/runtime/service/terminal", "github.com/wippyai/runtime/service/terminal"},
		{"api/service/websocket/errors.go", "service/websocket/errors.go", "github.com/wippyai/runtime/service/websocket", "github.com/wippyai/runtime/service/websocket"},
	}

	root := "wippy"

	for _, m := range migrations {
		fmt.Printf("Migrating %s -> %s\n", m.apiPath, m.servicePath)

		apiFullPath := filepath.Join(root, m.apiPath)
		serviceFullPath := filepath.Join(root, m.servicePath)

		// Check if API error file exists
		if _, err := os.Stat(apiFullPath); os.IsNotExist(err) {
			fmt.Printf("  Skipping: %s does not exist\n", m.apiPath)
			continue
		}

		// Check if service error file already exists
		if _, err := os.Stat(serviceFullPath); err == nil {
			fmt.Printf("  Skipping: %s already exists (needs manual merge)\n", m.servicePath)
			continue
		}

		// Create service directory if needed
		serviceDir := filepath.Dir(serviceFullPath)
		if err := os.MkdirAll(serviceDir, 0755); err != nil {
			fmt.Printf("  Error creating directory %s: %v\n", serviceDir, err)
			continue
		}

		// Copy error file
		if err := copyFile(apiFullPath, serviceFullPath); err != nil {
			fmt.Printf("  Error copying file: %v\n", err)
			continue
		}
		fmt.Printf("  Copied %s to %s\n", m.apiPath, m.servicePath)

		// Find all Go files that import the old package
		files := findFilesWithImport(root, m.oldPkg)

		if len(files) == 0 {
			fmt.Printf("  No files found importing %s\n", m.oldPkg)
			continue
		}

		// Update imports in each file
		for _, file := range files {
			// Skip files in api/service directory
			if strings.Contains(file, "/api/service/") {
				continue
			}

			if err := updateImportsInFile(file, m.oldPkg, m.newPkg); err != nil {
				fmt.Printf("  Error updating %s: %v\n", file, err)
			} else {
				fmt.Printf("  Updated imports in %s\n", file)
			}
		}
	}

	fmt.Println("\nMigration complete!")
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func findFilesWithImport(root, importPath string) []string {
	cmd := exec.Command("grep", "-r", "-l", importPath, "--include=*.go", root)
	output, err := cmd.Output()
	if err != nil {
		// grep returns error if no matches found
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var files []string
	for _, line := range lines {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

func updateImportsInFile(filePath, oldPkg, newPkg string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// Replace the import path
	newContent := bytes.ReplaceAll(content, []byte(oldPkg), []byte(newPkg))

	// Write back only if changed
	if !bytes.Equal(content, newContent) {
		return os.WriteFile(filePath, newContent, 0600)
	}

	return nil
}
