// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestToUint8_InRange asserts small integers coerce cleanly.
func TestToUint8_InRange(t *testing.T) {
	for _, v := range []any{5, int64(5), uint8(5), float64(5)} {
		got, ok := toUint8(v)
		assert.True(t, ok)
		assert.Equal(t, uint8(5), got)
	}
}

// TestToUint8_OutOfRange asserts values outside [0,255] fail instead of
// silently wrapping (e.g. 500 → 244).
func TestToUint8_OutOfRange(t *testing.T) {
	tests := []any{256, 500, int64(1000), -1, float64(300), float64(-1)}
	for _, v := range tests {
		got, ok := toUint8(v)
		assert.Falsef(t, ok, "toUint8(%v) must reject out-of-range input", v)
		assert.Equal(t, uint8(0), got)
	}
}

// TestToInt64_InRange asserts representable integers coerce cleanly.
func TestToInt64_InRange(t *testing.T) {
	for _, v := range []any{1, int32(2), int64(3), float64(4)} {
		got, ok := toInt64(v)
		assert.True(t, ok)
		assert.True(t, got >= 1 && got <= 4)
	}
}

// TestToInt64_FloatOverflow asserts floats outside int64 range are rejected.
func TestToInt64_FloatOverflow(t *testing.T) {
	for _, v := range []float64{math.MaxFloat64, -math.MaxFloat64, math.Inf(1), math.NaN()} {
		_, ok := toInt64(v)
		assert.Falsef(t, ok, "toInt64(%v) must reject non-representable float", v)
	}
}
