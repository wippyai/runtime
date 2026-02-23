// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"go.uber.org/zap"
)

func TestStateString(t *testing.T) {
	tests := []struct {
		expected string
		state    updateState
	}{
		{"pending", updatePending},
		{"accepted", updateAccepted},
		{"rejected", updateRejected},
		{"completed", updateComplete},
		{"unknown", updateState(99)},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := stateString(tt.state)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name       string
		defaultMsg string
		expected   string
		payloads   payload.Payloads
	}{
		{
			name:       "empty payloads returns default",
			payloads:   nil,
			defaultMsg: "default error",
			expected:   "default error",
		},
		{
			name:       "string payload returns string",
			payloads:   payload.Payloads{payload.NewString("custom error")},
			defaultMsg: "default",
			expected:   "custom error",
		},
		{
			name:       "non-string payload returns formatted",
			payloads:   payload.Payloads{payload.New(123)},
			defaultMsg: "default",
			expected:   "123",
		},
		{
			name:       "nil data payload returns formatted nil",
			payloads:   payload.Payloads{payload.New(nil)},
			defaultMsg: "default",
			expected:   "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractErrorMessage(tt.payloads, tt.defaultMsg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUpdateStateConstants(t *testing.T) {
	assert.NotEqual(t, updatePending, updateAccepted)
	assert.NotEqual(t, updateAccepted, updateRejected)
	assert.NotEqual(t, updateRejected, updateComplete)

	assert.Equal(t, updateState(0), updatePending)
	assert.Equal(t, updateState(1), updateAccepted)
	assert.Equal(t, updateState(2), updateRejected)
	assert.Equal(t, updateState(3), updateComplete)
}

func TestDefinitionFactory_WithContextCapturesIDs(t *testing.T) {
	ctx := temporalapi.WithClientID(context.Background(), "app.test.temporal:test_client")
	ctx = temporalapi.WithWorkerID(ctx, "app.test.temporal:test_worker")

	factory := &DefinitionFactory{
		ID:  registry.ParseID("app.test.temporal.workflows:update_workflow"),
		log: zap.NewNop(),
	}

	withCtx, ok := factory.WithContext(ctx).(*DefinitionFactory)
	assert.True(t, ok)
	assert.Equal(t, "app.test.temporal:test_client", withCtx.clientID)
	assert.Equal(t, "app.test.temporal:test_worker", withCtx.workerID)

	def, ok := withCtx.NewWorkflowDefinition().(*Definition)
	assert.True(t, ok)
	assert.Equal(t, "app.test.temporal:test_client", def.clientID)
	assert.Equal(t, "app.test.temporal:test_worker", def.workerID)
}

func TestDefinitionResolveIDs(t *testing.T) {
	t.Run("uses context IDs when available", func(t *testing.T) {
		ctx := temporalapi.WithClientID(context.Background(), "ctx-client")
		ctx = temporalapi.WithWorkerID(ctx, "ctx-worker")
		d := &Definition{
			ctx:      ctx,
			clientID: "captured-client",
			workerID: "captured-worker",
		}
		assert.Equal(t, "ctx-client", d.resolveClientID())
		assert.Equal(t, "ctx-worker", d.resolveWorkerID("fallback-worker"))
	})

	t.Run("falls back to captured IDs", func(t *testing.T) {
		d := &Definition{
			ctx:      context.Background(),
			clientID: "captured-client",
			workerID: "captured-worker",
		}
		assert.Equal(t, "captured-client", d.resolveClientID())
		assert.Equal(t, "captured-worker", d.resolveWorkerID("fallback-worker"))
	})

	t.Run("falls back to provided worker fallback", func(t *testing.T) {
		d := &Definition{ctx: context.Background()}
		assert.Equal(t, "", d.resolveClientID())
		assert.Equal(t, "fallback-worker", d.resolveWorkerID("fallback-worker"))
	})
}
