// SPDX-License-Identifier: MPL-2.0

package loader

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	"go.uber.org/zap"
)

// FilePayload represents a file-based payload that includes its source path
type FilePayload struct {
	payload.Payload
	path string
}

// Source returns the file path from which this payload was loaded
func (p *FilePayload) Source() string {
	return p.path
}

// FileLoader loads files, determines their format, and creates FilePayload objects.
type FileLoader struct {
	ext      map[string]payload.Format
	log      *zap.Logger
	skipDirs map[string]bool
}

// NewFileLoader creates a new FileLoader.
func NewFileLoader(log *zap.Logger) *FileLoader {
	skipDirs := map[string]bool{
		"node_modules": true,
	}

	// Skip temporal test directories when SKIP_TEMPORAL_TESTS is set.
	// Keep both names for compatibility across fixtures.
	if os.Getenv("SKIP_TEMPORAL_TESTS") != "" {
		skipDirs["_temporal"] = true
		skipDirs["temporal"] = true
		if log != nil {
			log.Info("SKIP_TEMPORAL_TESTS is set, skipping temporal directories")
		}
	}

	// Skip cloudstorage directory when SKIP_CLOUDSTORAGE_TESTS is set
	if os.Getenv("SKIP_CLOUDSTORAGE_TESTS") != "" {
		skipDirs["cloudstorage"] = true
		if log != nil {
			log.Info("SKIP_CLOUDSTORAGE_TESTS is set, skipping cloudstorage directories")
		}
	}

	// Skip SQS directory when SKIP_SQS_TESTS is set. These require a running
	// ElasticMQ (or LocalStack) container reachable at the configured endpoint.
	if os.Getenv("SKIP_SQS_TESTS") != "" {
		skipDirs["sqs"] = true
		if log != nil {
			log.Info("SKIP_SQS_TESTS is set, skipping sqs directories")
		}
	}

	// Skip network overlay tests when SKIP_NETWORK_TESTS is set. These
	// require a running docker-compose stack (socks5-proxy container).
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		skipDirs["network"] = true
		if log != nil {
			log.Info("SKIP_NETWORK_TESTS is set, skipping network directories")
		}
	}

	// Support SKIP_TEST_DIRS for arbitrary directory skipping (comma-separated)
	if dirs := os.Getenv("SKIP_TEST_DIRS"); dirs != "" {
		for _, dir := range strings.Split(dirs, ",") {
			dir = strings.TrimSpace(dir)
			if dir != "" {
				skipDirs[dir] = true
				if log != nil {
					log.Info("SKIP_TEST_DIRS: skipping directory", zap.String("dir", dir))
				}
			}
		}
	}

	return &FileLoader{
		ext: map[string]payload.Format{
			".json": payload.JSON,
			".yaml": payload.YAML,
			".yml":  payload.YAML,
		},
		log:      log,
		skipDirs: skipDirs,
	}
}

// shouldSkipDir returns true if the directory should be skipped
func (l *FileLoader) shouldSkipDir(name string) bool {
	return l.skipDirs[name]
}

// LoadFile loads a single file and returns its FilePayload
func (l *FileLoader) LoadFile(fSys fs.FS, path string) (*FilePayload, error) {
	ext := filepath.Ext(path)
	format, ok := l.ext[ext]
	if !ok {
		return nil, NewUnsupportedFileFormatError(path)
	}

	return l.loadFileAsPayload(fSys, path, format)
}

// LoadFS loads all supported files from FS and returns their FilePayloads
func (l *FileLoader) LoadFS(fSys fs.FS) ([]*FilePayload, error) {
	payloads := make([]*FilePayload, 0)
	err := fs.WalkDir(fSys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if l.shouldSkipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}

		// Skip files in directories that match skip patterns
		for skipDir := range l.skipDirs {
			if strings.Contains(path, skipDir+string(filepath.Separator)) ||
				strings.HasPrefix(path, skipDir+string(filepath.Separator)) {
				return nil
			}
		}

		ext := filepath.Ext(path)
		format, ok := l.ext[ext]
		if !ok {
			return nil // Skip unsupported file types
		}

		p, err := l.loadFileAsPayload(fSys, path, format)
		if err != nil {
			l.log.Error("load file as payload",
				zap.String("path", path),
				zap.Error(err))
			return nil
		}

		payloads = append(payloads, p)
		return nil
	})
	if err != nil {
		return nil, NewWalkFilesystemError(err)
	}

	return payloads, nil
}

func (l *FileLoader) LoadDir(fSys fs.FS, dirPath string) ([]*FilePayload, error) {
	payloads := make([]*FilePayload, 0)
	err := fs.WalkDir(fSys, dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if l.shouldSkipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}

		// Skip files in directories that match skip patterns
		for skipDir := range l.skipDirs {
			if strings.Contains(path, skipDir+string(filepath.Separator)) ||
				strings.HasPrefix(path, skipDir+string(filepath.Separator)) {
				return nil
			}
		}

		// Process only supported file types
		ext := filepath.Ext(path)
		format, ok := l.ext[ext]
		if !ok {
			return nil // Skip unsupported file types
		}

		p, err := l.loadFileAsPayload(fSys, path, format)
		if err != nil {
			l.log.Error("load file as payload",
				zap.String("path", path),
				zap.Error(err))
			return nil
		}

		payloads = append(payloads, p)
		return nil
	})
	if err != nil {
		return nil, NewWalkDirectoryError(dirPath, err)
	}

	return payloads, nil
}

// loadFileAsPayload loads the file content and creates a FilePayload.
func (l *FileLoader) loadFileAsPayload(fSys fs.FS, path string, format payload.Format) (*FilePayload, error) {
	file, err := fSys.Open(path)
	if err != nil {
		return nil, NewOpenFileError(path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			l.log.Error("close file", zap.String("path", path), zap.Error(closeErr))
		}
	}()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, NewReadFileError(path, err)
	}

	var p payload.Payload
	switch format {
	case payload.JSON:
		p = payload.NewPayload(data, payload.JSON)
	case payload.YAML:
		p = payload.NewPayload(data, payload.YAML)
	default:
		return nil, NewUnsupportedFormatError(format)
	}

	return &FilePayload{
		Payload: p,
		path:    path,
	}, nil
}
