// SPDX-License-Identifier: MPL-2.0

package entries

import "github.com/wippyai/runtime/boot/deps/wappextract"

// ExtractWappToDir extracts a .wapp file into a source directory with
// _index.yaml files and source files. After extraction, the .wapp file is
// removed.
func ExtractWappToDir(wappPath, targetDir string) error {
	return wappextract.ExtractWappToDir(wappPath, targetDir)
}

// ExtractWappToDirKeepSource extracts a module while retaining its canonical
// WAPP for resource-backed artifact reconciliation and repair.
func ExtractWappToDirKeepSource(wappPath, targetDir string) error {
	return wappextract.ExtractWappToDirKeepSource(wappPath, targetDir)
}
