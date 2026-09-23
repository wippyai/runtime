// SPDX-License-Identifier: MPL-2.0

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// ID is the identity of the exact content an executable ships. It names the
// deployment directory the bundle seeds, so two executables carrying different
// packs seed and keep separate deployments in one state directory.
func (bundle Bundle) ID() string {
	keys := make([]string, 0, len(bundle.Packs))
	for _, pack := range bundle.Packs {
		keys = append(keys, pack.Module+"@"+pack.Version+"="+pack.Digest)
	}
	sort.Strings(keys)
	sum := sha256.New()
	_, _ = sum.Write([]byte(bundle.Root + "\n"))
	for _, key := range keys {
		_, _ = sum.Write([]byte(key + "\n"))
	}
	return hex.EncodeToString(sum.Sum(nil))
}
