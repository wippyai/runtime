// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"os"
)

// syncPublication makes the rename durable by flushing the published file.
// Windows cannot flush a directory; NTFS records the rename in its journal, and
// flushing the file forces the journal, with that record, to stable storage.
func syncPublication(parent *os.Root, name string) error {
	f, err := parent.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
