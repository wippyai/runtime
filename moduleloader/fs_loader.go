package moduleloader

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// FilesystemLoader finds manifest with the given suffix in the directory.
type FilesystemLoader struct{}

func (FilesystemLoader) LoadManifest(_ context.Context) (*Manifest, error) {
	root, err := os.OpenRoot(".")
	if err != nil {
		return nil, fmt.Errorf("open root: %w", err)
	}

	var manifestName string
	err = fs.WalkDir(root.FS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk dir: %w", err)
		}

		if d.IsDir() {
			return nil
		}

		if strings.HasSuffix(d.Name(), ".wippy.yaml") || strings.HasSuffix(d.Name(), ".wippy.yml") {
			manifestName = d.Name()
			return fs.SkipAll
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk dir: %w", err)
	}

	if manifestName == "" {
		return nil, fmt.Errorf("no manifest found")
	}

	data, err := fs.ReadFile(root.FS(), manifestName)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var mf Manifest
	if err := yaml.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	return &mf, nil
}
