// SPDX-License-Identifier: MPL-2.0

package application

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

func bundleID(bundle Bundle) string {
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
