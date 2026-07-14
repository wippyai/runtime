// SPDX-License-Identifier: MPL-2.0
//go:build windows

package hub

// Windows does not support opening and syncing a directory through os.File.
// The artifact file itself is synced before publication.
func syncDirectory(string) error {
	return nil
}
