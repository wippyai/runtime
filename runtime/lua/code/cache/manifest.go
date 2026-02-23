// SPDX-License-Identifier: MPL-2.0

package cache

import "github.com/wippyai/go-lua/types/io"

// DecodeManifestSafe decodes a manifest and protects against panics from corrupt data.
func DecodeManifestSafe(data []byte) (*io.Manifest, bool) {
	if len(data) == 0 {
		return nil, false
	}
	defer func() {
		_ = recover()
	}()
	manifest, err := io.DecodeManifest(data)
	if err != nil {
		return nil, false
	}
	return manifest, true
}
