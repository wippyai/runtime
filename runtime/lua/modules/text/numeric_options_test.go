// SPDX-License-Identifier: MPL-2.0

package text

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestParseDiffOptionsNumericSubtypes(t *testing.T) {
	for name, source := range map[string]string{
		"integers": `return {
			diff_timeout = tonumber("2"),
			diff_edit_cost = string.len("abcdef"),
			match_threshold = tonumber("0.25"),
			match_distance = tonumber("100"),
			patch_delete_threshold = tonumber("1"),
			patch_margin = string.byte("\004"),
		}`,
		"floats": `return {
			diff_timeout = 2.0, diff_edit_cost = 6.0, match_threshold = 0.25,
			match_distance = 100.0, patch_delete_threshold = 1.0, patch_margin = 4.0,
		}`,
	} {
		t.Run(name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			if err := l.DoString(source); err != nil {
				t.Fatal(err)
			}
			got := parseDiffOptions(l.Get(-1).(*lua.LTable))
			want := DiffOptions{
				DiffTimeout: 2, DiffEditCost: 6, MatchThreshold: 0.25,
				MatchDistance: 100, PatchDeleteThreshold: 1, PatchMargin: 4,
			}
			if *got != want {
				t.Fatalf("got %+v, want %+v", *got, want)
			}
		})
	}
}
