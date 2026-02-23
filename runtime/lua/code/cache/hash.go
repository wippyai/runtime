// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// DepFingerprint links a dependency to its fingerprint.
type DepFingerprint struct {
	Alias       string
	ID          string
	Fingerprint string
}

// SourceHash matches code.HashNode (source+method).
func SourceHash(source, method string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(source))
	_, _ = h.Write([]byte(method))
	return hex.EncodeToString(h.Sum(nil))
}

// HashStrings hashes a sequence of strings with separators for stability.
func HashStrings(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Fingerprint hashes a self hash with ordered dependency fingerprints.
func Fingerprint(selfHash string, deps []DepFingerprint) string {
	ordered := make([]DepFingerprint, len(deps))
	copy(ordered, deps)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Alias != ordered[j].Alias {
			return ordered[i].Alias < ordered[j].Alias
		}
		if ordered[i].ID != ordered[j].ID {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Fingerprint < ordered[j].Fingerprint
	})

	h := sha256.New()
	_, _ = h.Write([]byte("self"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(selfHash))
	_, _ = h.Write([]byte{0})
	for _, dep := range ordered {
		_, _ = h.Write([]byte(dep.Alias))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(dep.ID))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(dep.Fingerprint))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
