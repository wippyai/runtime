package version

import (
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
)

// TestVersionMap_Path_Simple tests a simple linear path.
func TestVersionMap_Path_Simple(t *testing.T) {
	v1 := New(1, 0)
	v2 := FromParent(v1, 1, 1)
	v3 := FromParent(v2, 1, 2)

	vm := NewVersions()
	vm.Add(v1)
	vm.Add(v2)
	vm.Add(v3)

	from := v1
	to := v3
	actualPath := vm.Path(from, to)
	expectedPath := []registry.Version{v1, v2, v3}

	if !reflect.DeepEqual(actualPath, expectedPath) {
		t.Errorf("Expected path: %v, got: %v", expectedPath, actualPath)
	}
}

// TestVersionMap_Path_Backwards tests a path going backward in time.
func TestVersionMap_Path_Backwards(t *testing.T) {
	v1 := New(1, 0)
	v2 := FromParent(v1, 1, 1)
	v3 := FromParent(v2, 1, 2)

	vm := NewVersions()
	vm.Add(v1)
	vm.Add(v2)
	vm.Add(v3)

	// Go from v3 back to v1
	from := v3
	to := v1
	actualPath := vm.Path(from, to)
	expectedPath := []registry.Version{v3, v2, v1} // Path in reverse

	if !reflect.DeepEqual(actualPath, expectedPath) {
		t.Errorf("Expected path: %v, got: %v", expectedPath, actualPath)
	}
}

// TestVersionMap_Path_Branches tests a path across branches.
func TestVersionMap_Path_Branches(t *testing.T) {
	v1 := New(1, 0)
	v2 := FromParent(v1, 1, 1)
	v3 := FromParent(v2, 1, 2)
	v4 := FromParent(v2, 2, 0) // v4 branches from v2

	vm := NewVersions()
	vm.Add(v1)
	vm.Add(v2)
	vm.Add(v3)
	vm.Add(v4)

	// Go from v3 to v4 (across the branch)
	from := v3
	to := v4
	actualPath := vm.Path(from, to)
	expectedPath := []registry.Version{v3, v2, v4}

	if !reflect.DeepEqual(actualPath, expectedPath) {
		t.Errorf("Expected path: %v, got: %v", expectedPath, actualPath)
	}
}

func TestVersionMap(t *testing.T) {
	// Create some versions.
	v1 := New(1, 0)
	v2 := FromParent(v1, 1, 1)
	v3 := FromParent(v2, 1, 2)
	v4 := FromParent(v3, 1, 3)
	v5 := FromParent(v2, 2, 0) // v5 branches from v2

	// Test Cases
	testCases := []struct {
		name        string
		setup       func(vm registry.Versions) // Function to set up the versionMap
		from        registry.Version
		to          registry.Version
		expected    []registry.Version
		expectError bool
	}{
		{
			name: "Path within a branch",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
				vm.Add(v2)
				vm.Add(v3)
				vm.Add(v4)
			},
			from:     v1,
			to:       v4,
			expected: []registry.Version{v1, v2, v3, v4},
		},
		{
			name: "Path to the past",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
				vm.Add(v2)
				vm.Add(v3)
				vm.Add(v4)
			},
			from:     v4,
			to:       v1,
			expected: []registry.Version{v4, v3, v2, v1},
		},
		{
			name: "Path across branches",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
				vm.Add(v2)
				vm.Add(v3)
				vm.Add(v4)
				vm.Add(v5)
			},
			from:     v3,
			to:       v5,
			expected: []registry.Version{v3, v2, v5},
		},
		{
			name: "From and to are identical",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
			},
			from:     v1,
			to:       v1,
			expected: []registry.Version{v1},
		},
		{
			name: "From version not found",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
			},
			from:        v2,
			to:          v1,
			expected:    nil,
			expectError: true,
		},
		{
			name: "To version not found",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
			},
			from:        v1,
			to:          v2,
			expected:    nil,
			expectError: true,
		},
		{
			name: "No path exists",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
				vm.Add(New(2, 0)) // Create an unrelated version
			},
			from:        v1,
			to:          New(2, 0),
			expected:    nil,
			expectError: true,
		},
		{
			name: "Add and Get version",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
			},
			from:     v1,
			to:       v1,
			expected: []registry.Version{v1},
		},
		{
			name: "Delete version",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
				vm.Add(v2)
				vm.Delete(v1.ID())
			},
			from:        v2,
			to:          v1,
			expected:    nil,
			expectError: true,
		},
		{
			name: "Get non-existent version",
			setup: func(vm registry.Versions) {
				// No versions added
			},
			from:        v1,
			to:          v1,
			expected:    nil,
			expectError: true,
		},
		{
			name: "Len of empty version map",
			setup: func(vm registry.Versions) {
				// No versions added
			},
			from:     nil,
			to:       nil,
			expected: []registry.Version{}, // Expect empty slice, not nil
		},
		{
			name: "Range over versions",
			setup: func(vm registry.Versions) {
				vm.Add(v1)
				vm.Add(v2)
				vm.Add(v3)
			},
			from:     nil,
			to:       nil,
			expected: []registry.Version{v1, v2, v3}, // Expect all added versions
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			vm := NewVersions()

			if tc.setup != nil {
				tc.setup(vm)
			}

			switch tc.name {
			case "Add and Get version":
				got, ok := vm.Get(v1.ID())
				if !ok {
					t.Errorf("Expected version to be found, but it was not")
				}
				if !reflect.DeepEqual(got, v1) {
					t.Errorf("Expected to get version %v, got %v", v1, got)
				}
			case "Delete version":
				if _, ok := vm.Get(v1.ID()); ok {
					t.Errorf("Expected version to be deleted, but it was found")
				}
			case "Get non-existent version":
				if _, ok := vm.Get(v1.ID()); ok {
					t.Errorf("Expected to get no version, but one was found")
				}
			case "Len of empty version map":
				if vm.Len() != 0 {
					t.Errorf("Expected Len() to be 0, got %v", vm.Len())
				}
			case "Range over versions":
				var got []registry.Version
				vm.Range(func(id string, v registry.Version) bool {
					got = append(got, v)
					return true
				})
				if !reflect.DeepEqual(got, tc.expected) {
					t.Errorf("Expected to range over versions %v, got %v", tc.expected, got)
				}
			default:
				path := vm.Path(tc.from, tc.to)

				if tc.expectError {
					if path != nil {
						t.Errorf("Expected an error (path = nil), but got a path: %v", path)
					}
				} else {
					if !reflect.DeepEqual(path, tc.expected) {
						t.Errorf("Expected path: %v, got: %v", tc.expected, path)
					}
				}
			}
		})
	}
}
