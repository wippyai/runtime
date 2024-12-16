package version

import (
	"github.com/ponyruntime/pony/api/registry"
	"testing"
)

func TestVersion(t *testing.T) {
	testCases := []struct {
		name     string
		v1       registry.Version
		v2       registry.Version
		v1ID     uint
		v1PrevID uint
		v1String string
	}{
		{
			name:     "Equal versions",
			v1:       New(1),
			v2:       New(1),
			v1ID:     1,
			v1PrevID: 0,
			v1String: "v1",
		},
		{
			name:     "Large numbers",
			v1:       New(12345),
			v2:       New(12345),
			v1ID:     12345,
			v1PrevID: 0,
			v1String: "v12345",
		},
		{
			name:     "FromParent",
			v1:       FromParent(New(1), 2),
			v2:       New(2),
			v1ID:     2,
			v1PrevID: 1,
			v1String: "v2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test ID
			if v1ID := tc.v1.ID(); v1ID != tc.v1ID {
				t.Errorf("Expected ID() for %v to be %v, got %v", tc.v1, tc.v1ID, v1ID)
			}

			// Test Previous
			if prev := tc.v1.Previous(); prev != nil && prev.ID() != tc.v1PrevID {
				t.Errorf("Expected Previous() for %v to be %v, got %v", tc.v1, tc.v1PrevID, prev)
			}

			// Test String
			if v1String := tc.v1.String(); v1String != tc.v1String {
				t.Errorf("Expected String() for %v to be %v, got %v", tc.v1, tc.v1String, v1String)
			}
		})
	}
}
