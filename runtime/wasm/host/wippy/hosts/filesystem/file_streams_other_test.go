//go:build !windows

// SPDX-License-Identifier: MPL-2.0

package filesystem

import "os"

func openRetainedTestFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR, 0600)
}
