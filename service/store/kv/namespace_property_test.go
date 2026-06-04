// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"strings"
	"testing"
	"testing/quick"

	"github.com/wippyai/runtime/api/registry"
)

// validNS mirrors the app-namespace rule (leading lowercase letter, no ':').
func validNS(ns string) bool {
	if ns == "" || ns[0] < 'a' || ns[0] > 'z' {
		return false
	}
	return !strings.ContainsAny(ns, ":/ \t")
}

// TestProp_NamespaceRoundTripAndIsolation proves that for any namespace and key,
// physicalKey then logicalKey recovers the key, and no other namespace can read
// a key written under a different one (no cross-namespace leakage).
func TestProp_NamespaceRoundTripAndIsolation(t *testing.T) {
	f := func(ns, ns2, idstr string) bool {
		if !validNS(ns) || !validNS(ns2) || ns == ns2 {
			return true // only assert on two distinct valid namespaces
		}
		id := registry.ParseID(idstr)

		phys := physicalKey(ns, id)
		got, ok := logicalKey(ns, phys)
		if !ok || got.String() != id.String() {
			return false // own namespace must round-trip exactly
		}
		// A different namespace must NOT resolve this physical key.
		if _, leak := logicalKey(ns2, phys); leak {
			return false
		}
		// physicalPrefix of one namespace must never prefix the other's keys.
		return !strings.HasPrefix(phys, physicalPrefix(ns2, ""))
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatal(err)
	}
}
