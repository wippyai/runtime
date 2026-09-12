// SPDX-License-Identifier: MPL-2.0

package relay

import "testing"

func TestReleasePackageClearsConnectionProvenance(t *testing.T) {
	p := AcquirePackage()
	p.ReceivedFrom = "old-peer"
	ReleasePackage(p)
	// Inspect synchronously, with no other pool users; no dependence on which
	// object the pool might hand out next (or whether GC clears that pool).
	if p.ReceivedFrom != "" {
		t.Fatal("released package retained connection provenance")
	}
}
