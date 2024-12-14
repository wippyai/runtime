package path

import (
	"github.com/ponyruntime/pony/api/registry"
	"reflect"
	"testing"
)

func TestSortPathsHierarchically(t *testing.T) {
	testCases := []struct {
		name     string
		input    []registry.Path
		expected []registry.Path
	}{
		{
			name:     "empty_input",
			input:    []registry.Path{},
			expected: []registry.Path{},
		},
		{
			name: "single_element",
			input: []registry.Path{
				"only.entry",
			},
			expected: []registry.Path{
				"only.entry",
			},
		},
		{
			name: "flat_vs_hierarchical",
			input: []registry.Path{
				"x.y.z",
				"setting_x",
				"a.b.c",
				"standalone",
			},
			expected: []registry.Path{
				"setting_x",
				"standalone",
				"a.b.c",
				"x.y.z",
			},
		},
		{
			name: "mixed_hierarchy_levels",
			input: []registry.Path{
				"a.b.c",
				"a.b",
				"x.y.z",
				"x",
				"a",
				"x.y",
			},
			expected: []registry.Path{
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
			input: []registry.Path{
				"config.advanced.setting",
				"config",
				"config.basic",
				"config.advanced",
			},
			expected: []registry.Path{
				"config",
				"config.advanced",
				"config.basic",
				"config.advanced.setting",
			},
		},
		{
			name: "mixed_case_with_underscores",
			input: []registry.Path{
				"x.y.z",
				"setting_a",
				"a.b_c",
				"x.y",
				"standalone_value",
				"a.b_d",
			},
			expected: []registry.Path{
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
			input: []registry.Path{
				"b.setting_b",
				"z.setting_z",
				"c.nested.setting_c",
				"a.setting_a",
				"b.other_b",
				"setting_x",
			},
			expected: []registry.Path{
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
			input: []registry.Path{
				"y.x",
				"a.b.c",
				"x.y.z",
				"a.a.a.a",
				"a.d.e",
				"x.w.v",
			},
			expected: []registry.Path{
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
