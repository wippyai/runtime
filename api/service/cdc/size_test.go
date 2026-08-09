// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamOptionsMaxBytesDefaultsAndValidation(t *testing.T) {
	var options StreamOptions
	assert.Equal(t, DefaultMaxStreamBytes, options.EffectiveMaxBytes())
	assert.NoError(t, options.Validate())

	options.MaxBytes = 1024
	assert.Equal(t, int64(1024), options.EffectiveMaxBytes())
	assert.NoError(t, options.Validate())

	options.MaxBytes = -1
	assert.ErrorIs(t, options.Validate(), ErrInvalidMaxBytes)
}

func TestEstimateChangeBytesCountsNestedBlobs(t *testing.T) {
	change := Change{
		Source: "source",
		Before: map[string]any{
			"name":   "alice",
			"blob":   []byte{1, 2, 3, 4},
			"nested": map[string]any{"values": []any{"nested-value", []byte{5, 6}}},
		},
	}
	base := EstimateChangeBytes(Change{})
	got := EstimateChangeBytes(change)
	assert.Greater(t, got, base)
	assert.GreaterOrEqual(t, got-base, int64(len("alice")+4+len("nested-value")+2))
}

func TestEstimateChangeBytesTerminatesCyclicValues(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	change := Change{After: cyclic}

	assert.NotPanics(t, func() {
		assert.Positive(t, EstimateChangeBytes(change))
	})
}

func TestEstimateChangeBytesSaturates(t *testing.T) {
	assert.Equal(t, maxInt64Value, satAdd(maxInt64Value-1, 2))
	assert.Equal(t, maxInt64Value, satMul(maxInt64Value, 2))
	assert.LessOrEqual(t, EstimateChangeBytes(Change{}), maxInt64Value)
}

func TestEstimateChangeBytesBoundsDeepAndWideValues(t *testing.T) {
	deep := map[string]any{}
	current := deep
	for i := 0; i < maxEstimateDepth+2; i++ {
		next := map[string]any{}
		current["next"] = next
		current = next
	}
	assert.Equal(t, maxInt64Value, EstimateChangeBytes(Change{After: deep}))

	wide := make([]any, maxEstimateNodes+1)
	for i := range wide {
		wide[i] = "x"
	}
	assert.Equal(t, maxInt64Value, EstimateChangeBytes(Change{After: map[string]any{"wide": wide}}))
}
