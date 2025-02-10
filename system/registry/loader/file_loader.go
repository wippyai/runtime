package loader

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"go.uber.org/zap"
	"io/fs"
	"os"
	"path/filepath"
)

// FilePayload represents a file-based payload that includes its source path
type FilePayload struct {
	payload.Payload
	path string
}

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

// loadFileAsPayload loads the file content and creates a FilePayload.
func (l *FileLoader) loadFileAsPayload(path string, format payload.Format) (*FilePayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

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
