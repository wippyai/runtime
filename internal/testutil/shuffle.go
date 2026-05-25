// SPDX-License-Identifier: MPL-2.0

// Package testutil provides shared test helpers used across the runtime
// for determinism and shuffle-property style tests.
package testutil

import "math/rand/v2"

// ShuffleSlice returns a copy of s shuffled with a deterministic PCG source
// derived from seed. Calling ShuffleSlice with the same seed always yields the
// same permutation, so it is safe to use in regression tests that need to
// reproduce a specific input order.
func ShuffleSlice[T any](s []T, seed uint64) []T {
	out := make([]T, len(s))
	copy(out, s)
	r := rand.New(rand.NewPCG(seed, seed^0xA5A5A5A5A5A5A5A5)) //nolint:gosec // deterministic test shuffle, not security-sensitive
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}
