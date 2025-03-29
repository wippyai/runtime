package loader

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ponyruntime/pony/api/payload"
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
	ext   map[string]payload.Format
	log   *zap.Logger
	fsys  fs.FS // Optional filesystem to use
	useOS bool  // Whether to use OS filesystem operations
}

// FileLoaderOption is a function that configures a FileLoader
type FileLoaderOption func(*FileLoader)

// WithFS sets a custom filesystem for the FileLoader
func WithFS(fsys fs.FS) FileLoaderOption {
	return func(fl *FileLoader) {
		fl.fsys = fsys
		fl.useOS = false
	}
}

// NewFileLoader creates a new FileLoader with the given options.
func NewFileLoader(log *zap.Logger, opts ...FileLoaderOption) *FileLoader {
	fl := &FileLoader{
		ext: map[string]payload.Format{
			".json": payload.JSON,
			".yaml": payload.YAML,
			".yml":  payload.YAML,
		},
		log:   log,
		useOS: true, // Default to OS filesystem
	}

	// Apply options
	for _, opt := range opts {
		opt(fl)
	}

	return fl
}

// LoadFile loads a single file and returns its FilePayload
func (l *FileLoader) LoadFile(path string) (*FilePayload, error) {
	ext := filepath.Ext(path)
	format, ok := l.ext[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported file format for file %s", path)
	}

	return l.loadFileAsPayload(path, format)
}

// LoadFolder loads all supported files from a folder and returns their FilePayloads
func (l *FileLoader) LoadFolder(folderPath string) ([]*FilePayload, error) {
	if l.useOS {
		return l.loadFolderOS(folderPath)
	}
	return l.loadFolderFS(folderPath)
}

// loadFolderOS loads files using the OS filesystem
func (l *FileLoader) loadFolderOS(folderPath string) ([]*FilePayload, error) {
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("folder does not exist: %s", folderPath)
	}

	payloads := make([]*FilePayload, 0)
	err := filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
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

		p, err := l.loadFileAsPayload(path, format)
		if err != nil {
			l.log.Error("failed to load file as payload",
				zap.String("path", path),
				zap.Error(err))
			return nil
		}

		payloads = append(payloads, p)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory %s: %w", folderPath, err)
	}

	return payloads, nil
}

// loadFolderFS loads files using the provided fs.FS
func (l *FileLoader) loadFolderFS(rootPath string) ([]*FilePayload, error) {
	if rootPath == "" {
		rootPath = "."
	}

	payloads := make([]*FilePayload, 0)

	err := fs.WalkDir(l.fsys, rootPath, func(path string, d fs.DirEntry, err error) error {
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

		p, err := l.loadFileAsPayloadFS(path, format)
		if err != nil {
			l.log.Error("failed to load file as payload",
				zap.String("path", path),
				zap.Error(err))
			return nil
		}

		payloads = append(payloads, p)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory %s: %w", rootPath, err)
	}

	return payloads, nil
}

// loadFileAsPayload loads the file content and creates a FilePayload.
func (l *FileLoader) loadFileAsPayload(path string, format payload.Format) (*FilePayload, error) {
	if l.useOS {
		return l.loadFileAsPayloadOS(path, format)
	}
	return l.loadFileAsPayloadFS(path, format)
}

// loadFileAsPayloadOS loads files using the OS filesystem
func (l *FileLoader) loadFileAsPayloadOS(path string, format payload.Format) (*FilePayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return l.createFilePayload(path, data, format)
}

// loadFileAsPayloadFS loads files using the provided fs.FS
func (l *FileLoader) loadFileAsPayloadFS(path string, format payload.Format) (*FilePayload, error) {
	file, err := l.fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return l.createFilePayload(path, data, format)
}

// createFilePayload creates a FilePayload from data
func (l *FileLoader) createFilePayload(path string, data []byte, format payload.Format) (*FilePayload, error) {
	var p payload.Payload
	switch format {
	case payload.JSON:
		p = payload.NewPayload(data, payload.JSON)
	case payload.YAML:
		p = payload.NewPayload(data, payload.YAML)
	default:
		return nil, fmt.Errorf("unsupported file format: %s", format)
	}

	return &FilePayload{
		Payload: p,
		path:    path,
	}, nil
}
