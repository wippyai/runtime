package storage

import (
	"context"
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
		return ErrBasePathEmpty
	}

	fullBasePath := filepath.Join(s.baseDir, basePath)

	// Clean old version if exists (ensures no stale files from previous versions)
	if err := os.RemoveAll(fullBasePath); err != nil && !os.IsNotExist(err) {
		return NewCleanOldVersionError(basePath, err)
	}

	if len(files) == 0 {
		return nil
	}

	// Create fresh directory
	if err := os.MkdirAll(fullBasePath, 0755); err != nil {
		return NewCreateBaseDirectoryError(basePath, err)
	}

	hasLegacy := detectLegacyPrefix(files)

	for i, file := range files {
		if file == nil {
			return NewFileNilError(i)
		}

		filePath := file.GetPath()
		if filePath == "" {
			return NewFileEmptyPathError(i)
		}

		if hasLegacy {
			filePath = stripLegacyPrefix(filePath)
		}

		if filePath == "" {
			return NewFileEmptyPathAfterStripError(i)
		}

		if filepath.IsAbs(filePath) {
			return NewFileAbsolutePathError(i, filePath)
		}

		if !fs.ValidPath(filePath) {
			return NewFileInvalidPathError(i, filePath)
		}

		fullPath := filepath.Join(fullBasePath, filePath)

		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return NewCreateDirectoryError(filePath, err)
		}

		if err := os.WriteFile(fullPath, file.GetContent(), 0600); err != nil {
			return NewWriteFileError(filePath, err)
		}
	}

	return nil
}

// ReadFS returns an fs.FS interface for reading module files.
func (s *FileSystemStorage) ReadFS(basePath string) (fs.FS, error) {
	if basePath == "" {
		return nil, ErrBasePathEmpty
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewDirectoryNotExistError(basePath)
		}
		return nil, NewStatDirectoryError(basePath, err)
	}

	if !info.IsDir() {
		return nil, NewPathNotDirectoryError(basePath)
	}

	return os.DirFS(fullPath), nil
}

// Exists checks if a module directory exists and contains files.
func (s *FileSystemStorage) Exists(basePath string) (bool, error) {
	if basePath == "" {
		return false, ErrBasePathEmpty
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, NewStatDirectoryError(basePath, err)
	}

	if !info.IsDir() {
		return false, NewPathNotDirectoryError(basePath)
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return false, NewReadDirectoryError(basePath, err)
	}

	return len(entries) > 0, nil
}

// Delete removes a module directory completely.
func (s *FileSystemStorage) Delete(basePath string) error {
	if basePath == "" {
		return ErrBasePathEmpty
	}

	if basePath == "/" || basePath == "." || basePath == ".." {
		return NewRefuseDeleteProtectedPathError(basePath)
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	if err := os.RemoveAll(fullPath); err != nil {
		return NewRemoveDirectoryError(basePath, err)
	}
	return nil
}

// ComputeHash loads entries from a module directory and computes their hash.
func (s *FileSystemStorage) ComputeHash(ctx context.Context, basePath string, dtt payload.Transcoder, ldr Loader) (string, error) {
	if basePath == "" {
		return "", ErrBasePathEmpty
	}

	fullPath := filepath.Join(s.baseDir, basePath)

	dirFS := os.DirFS(fullPath)
	entries, err := ldr.LoadFS(ctx, dirFS)
	if err != nil {
		return "", NewLoadEntriesError(basePath, err)
	}

	hasher := hash.New(dtt)
	entryHash, err := hasher.Hash(entries)
	if err != nil {
		return "", NewComputeHashError(err)
	}

	return entryHash, nil
}
