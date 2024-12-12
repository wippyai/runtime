package version

import (
	"github.com/ponyruntime/pony/api/registry"
	"testing"
)

func TestVersion(t *testing.T) {
	testCases := []struct {
		name     string
		v1       Version
		v2       Version
		v1ID     string
		v1PrevID string
	}{
		{
			name:     "Equal versions",
			v1:       New(1, 2),
			v2:       New(1, 2),
			v1ID:     "v00001.002",
			v1PrevID: "",
		},
		{
			name:     "v1 less than v2 (major)",
			v1:       New(1, 2),
			v2:       New(2, 0),
			v1ID:     "v00001.002",
			v1PrevID: "",
		},
		{
			name:     "v1 less than v2 (minor)",
			v1:       New(1, 2),
			v2:       New(1, 3),
			v1ID:     "v00001.002",
			v1PrevID: "",
		},
		{
			name:     "v1 greater than v2 (major)",
			v1:       New(2, 2),
			v2:       New(1, 5),
			v1ID:     "v00002.002",
			v1PrevID: "",
		},
		{
			name:     "v1 greater than v2 (minor)",
			v1:       New(1, 5),
			v2:       New(1, 2),
			v1ID:     "v00001.005",
			v1PrevID: "",
		},
		{
			name:     "Large numbers",
			v1:       New(12345, 678),
			v2:       New(12345, 678),
			v1ID:     "v12345.678",
			v1PrevID: "",
		},
		{
			name:     "v1 less than v2 (large minor)",
			v1:       New(1, 999),
			v2:       New(2, 0),
			v1ID:     "v00001.999",
			v1PrevID: "",
		},
		{
			name:     "FromParent",
			v1:       FromParent(New(1, 2), 2, 3),
			v2:       New(2, 3),
			v1ID:     "v00002.003",
			v1PrevID: "v00001.002",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test ID
			if v1ID := tc.v1.ID(); v1ID != tc.v1ID {
				t.Errorf("Expected ID() for %v to be %v, got %v", tc.v1, tc.v1ID, v1ID)
			}

			// Test PreviousID
			if v1PrevID := tc.v1.PreviousID(); v1PrevID != tc.v1PrevID {
				t.Errorf("Expected PreviousID() for %v to be %v, got %v", tc.v1, tc.v1PrevID, v1PrevID)
			}

			// Test Interface Compliance (using v1)
			var v1Interface registry.Version = tc.v1

			if id := v1Interface.ID(); id != tc.v1.id {
				t.Errorf("Interface: Expected ID() to be %v, got %v", tc.v1.id, id)
			}

			if prevID := v1Interface.PreviousID(); prevID != tc.v1PrevID {
				t.Errorf("Interface: Expected PreviousID() to be %v, got %v", tc.v1PrevID, prevID)
			}
		})
	}
}
