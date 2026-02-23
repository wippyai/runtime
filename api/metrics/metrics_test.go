// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestMetricTypeConstants(t *testing.T) {
	assert.Equal(t, MetricType("counter"), TypeCounter)
	assert.Equal(t, MetricType("gauge"), TypeGauge)
	assert.Equal(t, MetricType("histogram"), TypeHistogram)
}

func TestContext_Collector(t *testing.T) {
	t.Run("with app context", func(t *testing.T) {
		ctx := ctxapi.NewRootContext()

		c := GetCollector(ctx)
		assert.Nil(t, c)

		type mockCollector struct{ Collector }
		mock := &mockCollector{}

		ctx = WithCollector(ctx, mock)

		retrieved := GetCollector(ctx)
		assert.Equal(t, mock, retrieved)
	})

	t.Run("without app context", func(t *testing.T) {
		ctx := context.Background()

		c := GetCollector(ctx)
		assert.Nil(t, c)

		type mockCollector struct{ Collector }
		mock := &mockCollector{}

		ctx = WithCollector(ctx, mock)
		assert.Equal(t, context.Background(), ctx)

		c = GetCollector(ctx)
		assert.Nil(t, c)
	})
}

func TestLabels(t *testing.T) {
	labels := Labels{"key": "value", "env": "test"}
	assert.Equal(t, "value", labels["key"])
	assert.Equal(t, "test", labels["env"])
}
