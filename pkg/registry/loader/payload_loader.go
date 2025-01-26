package loader

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponyruntime/pony/api/payload"
	"go.uber.org/zap"
)

// PayloadLoader loads files, determines their format, and creates payload.Payload objects.
type PayloadLoader struct {
	exts map[string]payload.Format
	log  *zap.Logger
}

// NewPayloadLoader creates a new PayloadLoader.
func NewPayloadLoader(log *zap.Logger) *PayloadLoader {
	return &PayloadLoader{
		exts: map[string]payload.Format{
			".json": payload.JSON,
			".yaml": payload.YAML,
			".yml":  payload.YAML,
		},
		log: log,
	}
}

// Load walks the directory tree from the rootPath, determines the format of files based on their extensions,
// and returns a map where the key is the relative dot-separated file path (without extension) and the value is the payload.Payload object.
func (l *PayloadLoader) Load(rootPath string) (map[string]payload.Payload, error) {
	payloads := make(map[string]payload.Payload)

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

		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err // Error getting relative path
		}

		// Transform to dot-separated path without extension
		dotSeparatedPath := strings.TrimSuffix(relPath, ext)

		if strings.Contains(dotSeparatedPath, ".") {
			dotSeparatedPath = strings.ReplaceAll(dotSeparatedPath, ".", "_")
		}

		dotSeparatedPath = filepath.ToSlash(dotSeparatedPath) // Ensure consistent slash direction

		p, err := l.loadFileAsPayload(path, format)
		if err != nil {
			l.log.Error("failed to load file as payload", zap.String("path", path), zap.Error(err))
			return nil
		}

		payloads[dotSeparatedPath] = p
		return nil
	})

	if err != nil {
		return nil, err
	}

	return payloads, nil
}

// loadFileAsPayload loads the file content and creates a Payload.
func (l *PayloadLoader) loadFileAsPayload(path string, format payload.Format) (payload.Payload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	switch format {
	case payload.JSON:
		return payload.NewPayload(data, payload.JSON), nil
	case payload.YAML:
		return payload.NewPayload(data, payload.YAML), nil
	default:
		return nil, fmt.Errorf("unsupported file format: %s", format)
	}
}
