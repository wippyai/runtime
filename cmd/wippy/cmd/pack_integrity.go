// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"

	"github.com/wippyai/wapp"
)

func verifyPackedResources(packPath string, resources []wapp.ResourceSpec) error {
	if len(resources) == 0 {
		return nil
	}

	file, err := os.Open(packPath)
	if err != nil {
		return fmt.Errorf("open generated pack: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return fmt.Errorf("open generated pack reader: %w", err)
	}

	for _, res := range resources {
		if err := verifyPackedResource(reader, res); err != nil {
			return err
		}
	}

	return nil
}

func verifyPackedResource(reader *wapp.Reader, res wapp.ResourceSpec) error {
	packedFS, err := reader.GetFS(res.ID)
	if err != nil {
		return fmt.Errorf("open packed resource %s: %w", res.ID.String(), err)
	}

	return fs.WalkDir(res.FS, ".", func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk source resource %s: %w", res.ID.String(), walkErr)
		}
		if d.IsDir() {
			return nil
		}

		source, err := fs.ReadFile(res.FS, filePath)
		if err != nil {
			return fmt.Errorf("read source resource %s file %q: %w", res.ID.String(), filePath, err)
		}

		packed, err := fs.ReadFile(packedFS, filePath)
		if err != nil {
			return fmt.Errorf("read packed resource %s file %q: %w", res.ID.String(), filePath, err)
		}

		if !bytes.Equal(packed, source) {
			return fmt.Errorf("packed resource %s file %q content mismatch: source=%d bytes packed=%d bytes",
				res.ID.String(), filePath, len(source), len(packed))
		}

		return nil
	})
}
