// SPDX-License-Identifier: MPL-2.0

package relay

import "testing"

func TestPackagePoolClearsIngressProvenance(t *testing.T) {
	p := AcquirePackage()
	p.IngressNode = "authenticated-peer"
	ReleasePackage(p)
	if p.IngressNode != "" {
		t.Fatal("released package retained the previous delivery's provenance")
	}
	q := AcquirePackage()
	defer ReleasePackage(q)
	if q.IngressNode != "" {
		t.Fatal("acquired package inherited another delivery's provenance")
	}
}
