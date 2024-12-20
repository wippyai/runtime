package path

import (
	"github.com/ponyruntime/pony/api/registry"
	"reflect"
	"testing"
)

func TestSortPathsHierarchically(t *testing.T) {
	testCases := []struct {
		name     string
		input    []registry.ID
		expected []registry.ID
	}{
		{
			name:     "empty_input",
			input:    []registry.ID{},
			expected: []registry.ID{},
		},
		{
			name: "single_element",
			input: []registry.ID{
				"only.entry",
			},
			expected: []registry.ID{
				"only.entry",
			},
		},
		{
			name: "flat_vs_hierarchical",
			input: []registry.ID{
				"x.y.z",
				"setting_x",
				"a.b.c",
				"standalone",
			},
			expected: []registry.ID{
				"setting_x",
				"standalone",
				"a.b.c",
				"x.y.z",
			},
		},
		{
			name: "mixed_hierarchy_levels",
			input: []registry.ID{
				"a.b.c",
				"a.b",
				"x.y.z",
				"x",
				"a",
				"x.y",
			},
			expected: []registry.ID{
				"a",
				"x",
				"a.b",
				"x.y",
				"a.b.c",
				"x.y.z",
			},
		},
		{
			name: "same_prefix_different_lengths",
			input: []registry.ID{
				"listener.advanced.setting",
				"listener",
				"listener.basic",
				"listener.advanced",
			},
			expected: []registry.ID{
				"listener",
				"listener.advanced",
				"listener.basic",
				"listener.advanced.setting",
			},
		},
		{
			name: "mixed_case_with_underscores",
			input: []registry.ID{
				"x.y.z",
				"setting_a",
				"a.b_c",
				"x.y",
				"standalone_value",
				"a.b_d",
			},
			expected: []registry.ID{
				"setting_a",
				"standalone_value",
				"a.b_c",
				"a.b_d",
				"x.y",
				"x.y.z",
			},
		},
		{
			name: "complex_mixed_case",
			input: []registry.ID{
				"b.setting_b",
				"z.setting_z",
				"c.nested.setting_c",
				"a.setting_a",
				"b.other_b",
				"setting_x",
			},
			expected: []registry.ID{
				"setting_x",
				"a.setting_a",
				"b.other_b",
				"b.setting_b",
				"z.setting_z",
				"c.nested.setting_c",
			},
		},
		{
			name: "sibling_paths_ordering_another_level",
			input: []registry.ID{
				"y.x",
				"a.b.c",
				"x.y.z",
				"a.a.a.a",
				"a.d.e",
				"x.w.v",
			},
			expected: []registry.ID{
				"y.x",
				"a.b.c",
				"a.d.e",
				"x.w.v",
				"x.y.z",
				"a.a.a.a",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := SortPaths(tc.input)
			if len(actual) == len(tc.expected) && len(actual) == 0 {
				return
			}

			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("\nInput:    %v\nExpected: %v\nGot:      %v", tc.input, tc.expected, actual)
			}
		})
	}
}
