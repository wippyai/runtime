package version

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/require"
)

// TestVersionMap_Path_Simple tests a simple linear path.
func TestVersionMap_Path_Simple(t *testing.T) {
	v1 := New(1)
	v2 := FromParent(v1, 2)
	v3 := FromParent(v2, 3)

	vm := NewVersionMap()
	require.NoError(t, vm.Add(v1))
	require.NoError(t, vm.Add(v2))
	require.NoError(t, vm.Add(v3))

	from := v1
	to := v3
	actualPath, _ := vm.Path(from, to)
	expectedPath := []registry.Version{v1, v2, v3}

	if !reflect.DeepEqual(actualPath, expectedPath) {
		t.Errorf("Expected path: %v, got: %v", expectedPath, actualPath)
	}
}

// TestVersionMap_Path_Backwards tests a path going backward in time.
func TestVersionMap_Path_Backwards(t *testing.T) {
	v1 := New(1)
	v2 := FromParent(v1, 2)
	v3 := FromParent(v2, 3)

	vm := NewVersionMap()
	require.NoError(t, vm.Add(v1))
	require.NoError(t, vm.Add(v2))
	require.NoError(t, vm.Add(v3))

	// Go from v3 back to v1
	from := v3
	to := v1
	actualPath, _ := vm.Path(from, to)
	expectedPath := []registry.Version{v3, v2, v1} // Name in reverse

	if !reflect.DeepEqual(actualPath, expectedPath) {
		t.Errorf("Expected path: %v, got: %v", expectedPath, actualPath)
	}
}

// TestVersionMap_Path_Branches tests a path across branches.
func TestVersionMap_Path_Branches(t *testing.T) {
	v1 := New(1)
	v2 := FromParent(v1, 2)
	v3 := FromParent(v2, 3)
	v4 := FromParent(v2, 4) // v4 branches from v2

	vm := NewVersionMap()
	require.NoError(t, vm.Add(v1))
	require.NoError(t, vm.Add(v2))
	require.NoError(t, vm.Add(v3))
	require.NoError(t, vm.Add(v4))

	// Go from v3 to v4 (across the branch)
	from := v3
	to := v4
	actualPath, _ := vm.Path(from, to)
	expectedPath := []registry.Version{v3, v2, v4}

	if !reflect.DeepEqual(actualPath, expectedPath) {
		t.Errorf("Expected path: %v, got: %v", expectedPath, actualPath)
	}
}

func TestVersionMap(t *testing.T) {
	// Create some versions.
	v1 := New(1)
	v2 := FromParent(v1, 2)
	v3 := FromParent(v2, 3)
	v4 := FromParent(v3, 4)
	v5 := FromParent(v2, 5) // v5 branches from v2

	// Test Cases
	testCases := []struct {
		name        string
		setup       func(vm Map) // Func to set up the versionHistory
		from        registry.Version
		to          registry.Version
		expected    []registry.Version
		expectError error
	}{
		{
			name: "Name within a branch",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
				require.NoError(t, vm.Add(v2))
				require.NoError(t, vm.Add(v3))
				require.NoError(t, vm.Add(v4))
			},
			from:     v1,
			to:       v4,
			expected: []registry.Version{v1, v2, v3, v4},
		},
		{
			name: "Name to the past",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
				require.NoError(t, vm.Add(v2))
				require.NoError(t, vm.Add(v3))
				require.NoError(t, vm.Add(v4))
			},
			from:     v4,
			to:       v1,
			expected: []registry.Version{v4, v3, v2, v1},
		},
		{
			name: "Name across branches",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
				require.NoError(t, vm.Add(v2))
				require.NoError(t, vm.Add(v3))
				require.NoError(t, vm.Add(v4))
				require.NoError(t, vm.Add(v5))
			},
			from:     v3,
			to:       v5,
			expected: []registry.Version{v3, v2, v5},
		},
		{
			name: "From and to are identical",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
			},
			from:     v1,
			to:       v1,
			expected: []registry.Version{v1},
		},
		{
			name: "From version not found",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
			},
			from:        v2,
			to:          v1,
			expected:    nil,
			expectError: fmt.Errorf("version %v not found", v2.ID()),
		},
		{
			name: "To version not found",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
			},
			from:        v1,
			to:          v2,
			expected:    nil,
			expectError: fmt.Errorf("version %v not found", v2.ID()),
		},
		{
			name: "No path exists",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
				require.NoError(t, vm.Add(New(2))) // Create an unrelated version
			},
			from:        v1,
			to:          New(2),
			expected:    nil,
			expectError: errors.New("no path exists from v1 to v2"),
		},
		{
			name: "Add and Create version",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
			},
			from:     v1,
			to:       v1,
			expected: []registry.Version{v1},
		},
		{
			name: "Create non-existent version",
			setup: func(Map) {
				// No versions added
			},
			from:        v1,
			to:          v1,
			expected:    nil,
			expectError: fmt.Errorf("version %v not found", v1.ID()),
		},
		{
			name: "Len of empty version map",
			setup: func(Map) {
				// No versions added
			},
			from:     nil,
			to:       nil,
			expected: []registry.Version{}, // Expect empty slice, not nil
		},
		{
			name: "Range over versions",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v1))
				require.NoError(t, vm.Add(v2))
				require.NoError(t, vm.Add(v3))
			},
			from:     nil,
			to:       nil,
			expected: []registry.Version{v1, v2, v3}, // Expect all added versions
		},
		{
			name: "Range over versions",
			setup: func(vm Map) {
				require.NoError(t, vm.Add(v3))
				require.NoError(t, vm.Add(v2))
				require.NoError(t, vm.Add(v5))
				require.NoError(t, vm.Add(v1))
			},
			from:     nil,
			to:       nil,
			expected: []registry.Version{v1, v2, v3, v5}, // Expect all added versions
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			vm := NewVersionMap()

			if tc.setup != nil {
				tc.setup(vm)
			}

			switch tc.name {
			case "Len of empty version map":
				if vm.Len() != 0 {
					t.Errorf("Expected Len() to be 0, got %v", vm.Len())
				}
			case "Range over versions":
				var got []registry.Version
				vm.Range(func(_ uint, v registry.Version) bool {
					got = append(got, v)
					return true
				})
				if !reflect.DeepEqual(got, tc.expected) {
					t.Errorf("Expected to range over versions %v, got %v", tc.expected, got)
				}
			default:
				path, err := vm.Path(tc.from, tc.to)

				if tc.expectError != nil {
					if err == nil {
						t.Errorf("Expected an error '%v', but got none", tc.expectError)
					} else if err.Error() != tc.expectError.Error() {
						t.Errorf("Expected error '%v', got '%v'", tc.expectError, err)
					}
					if path != nil {
						t.Errorf("Expected path to be nil due to error, but got: %v", path)
					}
				} else {
					if err != nil {
						t.Errorf("Expected no error, but got: %v", err)
					}
					if !reflect.DeepEqual(path, tc.expected) {
						t.Errorf("Expected path: %v, got: %v", tc.expected, path)
					}
				}
			}
		})
	}
}
