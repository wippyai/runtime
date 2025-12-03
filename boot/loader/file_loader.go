package loader

import (
	"io"
	"io/fs"
	"path/filepath"

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
	ext map[string]payload.Format
	log *zap.Logger
}

// NewFileLoader creates a new FileLoader.
func NewFileLoader(log *zap.Logger) *FileLoader {
	return &FileLoader{
		ext: map[string]payload.Format{
			".json": payload.JSON,
			".yaml": payload.YAML,
			".yml":  payload.YAML,
		},
		log: log,
	}
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
			return nil // Skip directories
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
			// Continue walking through all directories
			return nil
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
	case payload.String, payload.Golang, payload.Lua, payload.Bytes, payload.Error:
		// FIXME implement
	default:
		return nil, NewUnsupportedFormatError(string(format))
	}

	return &FilePayload{
		Payload: p,
		path:    path,
	}, nil
}
