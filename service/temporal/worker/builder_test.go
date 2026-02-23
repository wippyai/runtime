// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.uber.org/zap"
)

func TestWorkerBuilder_Build_RequiresConfig(t *testing.T) {
	_, err := NewWorkerBuilder().
		WithTranscoder(newWorkerTestTranscoder()).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is required")
}

func TestWorkerBuilder_Build_RequiresTranscoder(t *testing.T) {
	_, err := NewWorkerBuilder().
		WithConfig(&api.WorkerConfig{}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transcoder is required")
}

func TestWorkerBuilder_Build_DefaultLogger(t *testing.T) {
	w, err := NewWorkerBuilder().
		WithConfig(&api.WorkerConfig{}).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()
	require.NoError(t, err)
	require.NotNil(t, w)
	assert.NotNil(t, w.log)
}

func TestWorkerBuilder_Build_CustomLogger(t *testing.T) {
	logger := zap.NewNop()
	w, err := NewWorkerBuilder().
		WithLogger(logger).
		WithConfig(&api.WorkerConfig{}).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()
	require.NoError(t, err)
	assert.Equal(t, logger, w.log)
}

func TestWorkerBuilder_Build_SetsAllFields(t *testing.T) {
	logger := zap.NewNop()
	id := registry.ID{NS: "test", Name: "worker-1"}
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "my-queue",
	}
	resReg := &mockResourceRegistry{}
	transcoder := newWorkerTestTranscoder()

	w, err := NewWorkerBuilder().
		WithLogger(logger).
		WithID(id).
		WithConfig(cfg).
		WithResourceRegistry(resReg).
		WithTranscoder(transcoder).
		Build()
	require.NoError(t, err)
	require.NotNil(t, w)

	assert.Equal(t, id, w.id)
	assert.Equal(t, cfg, w.config)
	assert.Equal(t, resReg, w.resourceReg)
	assert.Equal(t, transcoder, w.dtt)
	assert.NotNil(t, w.activities)
	assert.NotNil(t, w.workflows)
	assert.NotNil(t, w.pidGen)
}

func TestWorkerBuilder_Fluent(t *testing.T) {
	b := NewWorkerBuilder()
	assert.Equal(t, b, b.WithLogger(zap.NewNop()))
	assert.Equal(t, b, b.WithID(registry.ID{}))
	assert.Equal(t, b, b.WithConfig(&api.WorkerConfig{}))
	assert.Equal(t, b, b.WithResourceRegistry(nil))
	assert.Equal(t, b, b.WithEnvRegistry(nil))
	assert.Equal(t, b, b.WithInterceptors(nil))
	assert.Equal(t, b, b.WithTranscoder(nil))
}
