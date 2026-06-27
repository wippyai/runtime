// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func TestStore_Telemetry_EmitsOpsMetrics(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t, "teltest")
	rec := telemetrytest.NewRecorder()
	s.coll = rec

	key := registry.ParseID("app:k1")
	require.NoError(t, s.Set(ctx, store.Entry{Key: key, Value: jsonVal(`"v1"`)}))
	_, err := s.Get(ctx, key)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, key))
	_, _ = s.Get(ctx, key)

	assert.Equal(t, 1.0, rec.CounterValue(kvOpsTotal, metrics.Labels{"namespace": "teltest", "op": "set", "result": "ok"}))
	assert.Equal(t, 1.0, rec.CounterValue(kvOpsTotal, metrics.Labels{"namespace": "teltest", "op": "get", "result": "ok"}))
	assert.Equal(t, 1.0, rec.CounterValue(kvOpsTotal, metrics.Labels{"namespace": "teltest", "op": "delete", "result": "ok"}))
	assert.Equal(t, 1.0, rec.CounterValue(kvOpsTotal, metrics.Labels{"namespace": "teltest", "op": "get", "result": "not_found"}))
	assert.Equal(t, uint64(1), rec.HistogramCount(kvOpDuration, metrics.Labels{"namespace": "teltest", "op": "get", "result": "ok"}))
}
