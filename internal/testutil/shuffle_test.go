// SPDX-License-Identifier: MPL-2.0

package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShuffleSlice_Reproducible(t *testing.T) {
	input := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	first := ShuffleSlice(input, 42)
	second := ShuffleSlice(input, 42)
	require.Equal(t, first, second, "same seed must produce identical permutation")

	other := ShuffleSlice(input, 43)
	assert.NotEqual(t, first, other, "different seeds should produce different permutations")
}

func TestShuffleSlice_DoesNotMutateInput(t *testing.T) {
	input := []int{1, 2, 3, 4, 5}
	original := append([]int(nil), input...)

	_ = ShuffleSlice(input, 7)

	assert.Equal(t, original, input, "input must not be mutated")
}

func TestShuffleSlice_PreservesElements(t *testing.T) {
	input := []string{"a", "b", "c", "d", "e", "f"}

	for seed := uint64(0); seed < 100; seed++ {
		out := ShuffleSlice(input, seed)
		require.Len(t, out, len(input))
		seen := make(map[string]int, len(input))
		for _, v := range out {
			seen[v]++
		}
		for _, v := range input {
			require.Equal(t, 1, seen[v], "element %q must appear exactly once for seed %d", v, seed)
		}
	}
}
