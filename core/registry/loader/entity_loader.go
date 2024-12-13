package loader

import (
	"fmt"
	"go.uber.org/zap"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ponyruntime/pony/api/payload"
)

// EntryLoader loads files, determines their format, and creates payload.Payload objects.
type EntryLoader struct {
	exts map[string]payload.Format
	zap  *zap.Logger
}

// NewEntryLoader creates a new EntryLoader.
func NewEntryLoader(zap *zap.Logger) *EntryLoader {
	return &EntryLoader{
		exts: map[string]payload.Format{
			".json": payload.Json,
			".yaml": payload.Yaml,
			".yml":  payload.Yaml,
		},
		zap: zap,
	}
}

// Load walks the directory tree from the rootPath, determines the format of files based on their extensions,
// and returns a slice of payload.Payload objects representing the file contents.
func (l *EntryLoader) Load(rootPath string) ([]payload.Payload, error) {
	var payloads []payload.Payload

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil // Skip directories
		}

		ext := filepath.Ext(path)
		format, ok := l.exts[ext]
		if !ok {
			return nil // Skip unsupported file types
		}

		p, err := l.loadFileAsPayload(path, format)
		if err != nil {
			l.zap.Error("failed to load file as payload", zap.String("path", path), zap.Error(err))
			return nil
		}

		payloads = append(payloads, p)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return payloads, nil
}

// loadFileAsPayload loads the file content and creates a Payload.
func (l *EntryLoader) loadFileAsPayload(path string, format payload.Format) (payload.Payload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	switch format {
	case payload.Json:
		return payload.NewPayload(data, payload.Json), nil
	case payload.Yaml:
		return payload.NewPayload(data, payload.Yaml), nil
	default:
		return nil, fmt.Errorf("unsupported file format: %s", format)
	}
}
