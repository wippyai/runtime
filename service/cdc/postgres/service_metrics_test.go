// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
	"go.uber.org/zap"
)

func TestSource_FailEmitsErrorCounter(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	s := &Source{log: zap.NewNop(), name: "test:source", slot: "test_slot", coll: rec}

	status := make(chan any, 1)
	s.fail(context.Background(), status, errors.New("boom"))

	assert.Equal(t, 1.0, rec.CounterValue(errorsCounter, metrics.Labels{"source": "test:source"}))
}

func TestSource_FailNilCollector(t *testing.T) {
	s := &Source{log: zap.NewNop(), slot: "test_slot"}
	status := make(chan any, 1)
	s.fail(context.Background(), status, errors.New("boom"))
}
