//go:build linux || darwin

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"os"
)

// syncPublication makes the rename durable by syncing the directory that
// holds the published entry.
func syncPublication(parent *os.Root, _ string) error {
	f, err := parent.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
