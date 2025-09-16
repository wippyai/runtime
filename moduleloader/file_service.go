package moduleloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
	"go.uber.org/zap"
)

// FileService handles file operations for modules
type FileService struct {
	logger *zap.Logger
}

// NewFileService creates a new file service
func NewFileService(logger *zap.Logger) *FileService {
	return &FileService{logger: logger}
}

// StoreModuleFiles stores module files in the given directory
func (fs *FileService) StoreModuleFiles(moduleDir string, files []*modulev1.File) error {
	if len(files) == 0 {
		return nil
	}

	// Get subdirectory name from first file
	firstPath := files[0].GetPath()
	pathParts := strings.Split(firstPath, "/")
	if len(pathParts) == 0 {
		return fmt.Errorf("invalid file path: %s", firstPath)
	}

	moduleSubdir := pathParts[0]
	subdirPath := filepath.Join(moduleDir, moduleSubdir)

	// Create subdirectory
	if err := os.MkdirAll(subdirPath, os.ModePerm); err != nil {
		return fmt.Errorf("create module subdirectory: %w", err)
	}

	// Write files to subdirectory
	for _, file := range files {
		if err := fs.writeFile(subdirPath, file, moduleSubdir); err != nil {
			return fmt.Errorf("write file %s: %w", file.GetPath(), err)
		}
	}

	return nil
}

// writeFile writes a single file to the module directory
func (fs *FileService) writeFile(subdirPath string, file *modulev1.File, moduleSubdir string) error {
	filePath := file.GetPath()

	// Remove module subdirectory prefix if present
	if strings.HasPrefix(filePath, moduleSubdir+"/") {
		filePath = strings.TrimPrefix(filePath, moduleSubdir+"/")
	}

	fullPath := filepath.Join(subdirPath, filePath)

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(fullPath, file.GetContent(), 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// FindModuleSubdir finds the subdirectory where module files are stored
func (fs *FileService) FindModuleSubdir(moduleDir string) (string, error) {
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return "", fmt.Errorf("read module directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "module-") {
			return filepath.Join(moduleDir, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no module subdirectory found in %s", moduleDir)
}

// EnsureDirectoryExists creates a directory if it doesn't exist
func (fs *FileService) EnsureDirectoryExists(path string) error {
	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return nil
}
