// SPDX-License-Identifier: MPL-2.0

package registry

import "github.com/wippyai/runtime/api/attrs"

// EntryOptionsMetaKey is the registry metadata bag holding declaration-time
// runtime options. Entry data remains the executable/config payload.
const EntryOptionsMetaKey = "options"

// EntryOptionsFromMeta returns the nested meta.options bag.
func EntryOptionsFromMeta(meta attrs.Bag) (attrs.Bag, bool) {
	if meta == nil {
		return nil, false
	}
	return meta.GetBag(EntryOptionsMetaKey)
}
