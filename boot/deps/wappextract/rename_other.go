// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package wappextract

import "os"

// RenameDir renames a directory.
func RenameDir(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}
