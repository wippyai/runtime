// SPDX-License-Identifier: MPL-2.0
//go:build !windows

package hub

import (
	"fmt"
	"os"
)

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open artifact cache directory for sync: %w", err)
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return fmt.Errorf("sync artifact cache directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close artifact cache directory: %w", closeErr)
	}
	return nil
}
