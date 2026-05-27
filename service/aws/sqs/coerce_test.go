// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestToInt32_InRange asserts representable integers coerce cleanly.
func TestToInt32_InRange(t *testing.T) {
	for _, v := range []any{1, int32(2), int64(3), float64(4)} {
		got, ok := toInt32(v)
		assert.True(t, ok)
		assert.True(t, got >= 1 && got <= 4)
	}
}

// TestToInt32_OutOfRange asserts values outside int32 range are rejected
// rather than silently truncated.
func TestToInt32_OutOfRange(t *testing.T) {
	tests := []any{
		int64(math.MaxInt32) + 1,
		int64(math.MinInt32) - 1,
		int64(5_000_000_000),
		float64(math.MaxInt32) + 1,
		float64(math.MinInt32) - 1,
		math.Inf(1),
		math.NaN(),
	}
	for _, v := range tests {
		got, ok := toInt32(v)
		assert.Falsef(t, ok, "toInt32(%v) must reject out-of-range input", v)
		assert.Equal(t, int32(0), got)
	}
}
