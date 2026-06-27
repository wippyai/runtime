// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func TestProcessLifecycle_StartAndComplete(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	p := NewProcessLifecycle(rec)

	require.NoError(t, p.OnStart(context.Background(), pid.PID{}, nil))
	require.NoError(t, p.OnStart(context.Background(), pid.PID{}, nil))
	p.OnComplete(context.Background(), pid.PID{}, &runtime.Result{})

	assert.Equal(t, 2.0, rec.CounterValue(ProcessStarted, metrics.Labels{}))
	assert.Equal(t, 1.0, rec.CounterValue(ProcessTerminated, metrics.Labels{"result": "completed"}))
	assert.Equal(t, 1.0, rec.GaugeValue(ProcessActive, metrics.Labels{}))
}

func TestProcessLifecycle_ErrorResult(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	p := NewProcessLifecycle(rec)

	p.OnComplete(context.Background(), pid.PID{}, &runtime.Result{Error: errors.New("boom")})

	assert.Equal(t, 1.0, rec.CounterValue(ProcessTerminated, metrics.Labels{"result": "error"}))
}

func TestProcessLifecycle_NilCollector(t *testing.T) {
	p := NewProcessLifecycle(nil)
	require.NoError(t, p.OnStart(context.Background(), pid.PID{}, nil))
	p.OnComplete(context.Background(), pid.PID{}, nil)
}
