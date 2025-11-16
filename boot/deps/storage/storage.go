package storage

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/hash"
)

// Loader interface for loading entries from filesystem
type Loader interface {
	LoadFS(ctx context.Context, fsys fs.FS) ([]regapi.Entry, error)
}

// Storage handles module file I/O operations.
type Storage interface {
	// StoreProtoFiles stores protobuf files to the storage.
	// Handles legacy "module-*/" prefix automatically (strips it).
	// basePath: target directory (e.g., .wippy/vendor/org/module)
	// files: protobuf files from download service
	StoreProtoFiles(basePath string, files []*modulev1.File) error

	// ReadFS returns an fs.FS interface for reading module files.
	// basePath: module directory (e.g., .wippy/vendor/org/module)
	ReadFS(basePath string) (fs.FS, error)

	// Exists checks if a module directory exists and contains files.
	Exists(basePath string) (bool, error)

	// Delete removes a module directory completely.
	Delete(basePath string) error
}

// FileSystemStorage implements Storage using the OS filesystem.
type FileSystemStorage struct {
	baseDir string
}

// NewFileSystemStorage creates a new filesystem storage.
// baseDir: root directory for all storage operations (e.g., .wippy/vendor)
func NewFileSystemStorage(baseDir string) *FileSystemStorage {
	return &FileSystemStorage{
		baseDir: baseDir,
	}
}

// StoreProtoFiles stores protobuf files to disk, handling legacy prefix.
// Cleans the target directory before writing to ensure only new version files remain.
func (s *FileSystemStorage) StoreProtoFiles(basePath string, files []*modulev1.File) error {
	if basePath == "" {
		return fmt.Errorf("basePath cannot be empty")
	}

	fullBasePath := filepath.Join(s.baseDir, basePath)

	// Clean old version if exists (ensures no stale files from previous versions)
	if err := os.RemoveAll(fullBasePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean old version at %q: %w", basePath, err)
	}

	if len(files) == 0 {
		return nil
	}

	// Create fresh directory
	if err := os.MkdirAll(fullBasePath, 0755); err != nil {
		return fmt.Errorf("create base directory %q: %w", basePath, err)
	}

	hasLegacy := detectLegacyPrefix(files)

	for i, file := range files {
		if file == nil {
			return fmt.Errorf("file at index %d is nil", i)
		}

		filePath := file.GetPath()
		if filePath == "" {
			return fmt.Errorf("file at index %d has empty path", i)
		}

		if hasLegacy {
			filePath = stripLegacyPrefix(filePath)
		}

		if filePath == "" {
			return fmt.Errorf("file at index %d has empty path after stripping legacy prefix", i)
		}

		if filepath.IsAbs(filePath) {
			return fmt.Errorf("file at index %d has absolute path %q, expected relative path", i, filePath)
		}

		if !fs.ValidPath(filePath) {
			return fmt.Errorf("file at index %d has invalid path %q", i, filePath)
		}

		fullPath := filepath.Join(fullBasePath, filePath)

		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("create directory for %q: %w", filePath, err)
		}

		if err := os.WriteFile(fullPath, file.GetContent(), 0600); err != nil {
			return fmt.Errorf("write file %q: %w", filePath, err)
		}
	}

	return nil
}

// ReadFS returns an fs.FS interface for reading module files.
func (s *FileSystemStorage) ReadFS(basePath string) (fs.FS, error) {
	if basePath == "" {
		return nil, fmt.Errorf("basePath cannot be empty")
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory %q does not exist", basePath)
		}
		return nil, fmt.Errorf("stat directory %q: %w", basePath, err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("path %q is not a directory", basePath)
	}

	return os.DirFS(fullPath), nil
}

// Exists checks if a module directory exists and contains files.
func (s *FileSystemStorage) Exists(basePath string) (bool, error) {
	if basePath == "" {
		return false, fmt.Errorf("basePath cannot be empty")
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat directory %q: %w", basePath, err)
	}

	if !info.IsDir() {
		return false, fmt.Errorf("path %q exists but is not a directory", basePath)
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return false, fmt.Errorf("read directory %q: %w", basePath, err)
	}

	return len(entries) > 0, nil
}

// Delete removes a module directory completely.
func (s *FileSystemStorage) Delete(basePath string) error {
	if basePath == "" {
		return fmt.Errorf("basePath cannot be empty")
	}

	if basePath == "/" || basePath == "." || basePath == ".." {
		return fmt.Errorf("refusing to delete protected path %q", basePath)
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	if err := os.RemoveAll(fullPath); err != nil {
		return fmt.Errorf("remove directory %q: %w", basePath, err)
	}
	return nil
}

// ComputeHash loads entries from a module directory and computes their hash.
func (s *FileSystemStorage) ComputeHash(ctx context.Context, basePath string, dtt payload.Transcoder, ldr Loader) (string, error) {
	if basePath == "" {
		return "", fmt.Errorf("basePath cannot be empty")
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	dirFS := os.DirFS(fullPath)
	entries, err := ldr.LoadFS(ctx, dirFS)
	if err != nil {
		return "", fmt.Errorf("load entries from %s: %w", basePath, err)
	}

	hasher := hash.New(dtt)
	entryHash, err := hasher.Hash(entries)
	if err != nil {
		return "", fmt.Errorf("compute hash: %w", err)
	}

	return entryHash, nil
}
