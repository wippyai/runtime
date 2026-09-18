// SPDX-License-Identifier: MPL-2.0

package cachedir

import (
	"os"
)

// Probe verifies that dir can be created and written.
func Probe(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return err
	}
	probeName := f.Name()
	_ = f.Close()
	return os.Remove(probeName)
}
