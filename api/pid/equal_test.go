// SPDX-License-Identifier: MPL-2.0

package pid

import "testing"

func TestEqualIgnoresCacheButChecksEveryIdentityField(t *testing.T) {
	original := PID{Node: "node", Host: "host", UniqID: "instance"}
	cached := original.Precomputed()
	if !original.Equal(cached) || !cached.Equal(original) {
		t.Fatal("cache changed identity")
	}
	for _, field := range []string{"node", "host", "instance"} {
		changed := cached
		switch field {
		case "node":
			changed.Node = "different"
		case "host":
			changed.Host = "different"
		case "instance":
			changed.UniqID = "different"
		}
		if original.Equal(changed) || changed.Equal(original) {
			t.Fatalf("ignored changed %s", field)
		}
	}
	if !(PID{}).Equal(PID{}) {
		t.Fatal("zero identities differ")
	}
}
