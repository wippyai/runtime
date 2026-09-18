// SPDX-License-Identifier: MPL-2.0
//go:build !windows

package hub

import (
	"os"
)

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return NewArtifactIOError("open artifact cache directory for sync", path, err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return NewArtifactIOError("sync artifact cache directory", path, syncErr)
	}
	if closeErr != nil {
		return NewArtifactIOError("close artifact cache directory", path, closeErr)
	}
	return nil
}
