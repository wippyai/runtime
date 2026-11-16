// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	pgapi "github.com/wippyai/runtime/api/service/pg"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func assertTransientUnavailable(t *testing.T, err error, want error) {
	t.Helper()
	require.ErrorIs(t, err, want)
	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, apierror.Unavailable, apiErr.Kind())
	assert.Equal(t, apierror.True, apiErr.Retryable())
}

func TestServiceAcquireBeforeStartIsNotStarted(t *testing.T) {
	svc, _, _ := newTestService()

	res, err := svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	assert.Nil(t, res)
	assertTransientUnavailable(t, err, ErrServiceNotStarted)
	assert.NotErrorIs(t, err, ErrServiceStopped)
}

func TestServiceAcquireAfterStopIsStopped(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, svc.Stop(context.Background()))

	res, err := svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	assert.Nil(t, res)
	assertTransientUnavailable(t, err, ErrServiceStopped)
	assert.NotErrorIs(t, err, ErrServiceNotStarted)
}

func TestServiceAcquireAfterStartContextCancelIsStopped(t *testing.T) {
	svc, _, _ := newTestService()
	ctx, cancel := context.WithCancel(context.Background())
	_, err := svc.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })
	cancel()

	res, err := svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	assert.Nil(t, res)
	assertTransientUnavailable(t, err, ErrServiceStopped)
}

func TestServiceAcquireServesOnceStarted(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	require.ErrorIs(t, err, ErrServiceNotStarted)

	_, err = svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	res, err := svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	require.NoError(t, err)
	defer res.Release()
	got, err := res.Get()
	require.NoError(t, err)
	assert.Same(t, svc, got)
}

func TestServiceAcquireServesAfterRestart(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, svc.Stop(context.Background()))
	_, err = svc.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Stop(context.Background()) })

	res, err := svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	require.NoError(t, err)
	res.Release()
}

func TestServiceStopBeforeStartIsStopped(t *testing.T) {
	svc, _, _ := newTestService()
	require.NoError(t, svc.Stop(context.Background()))

	_, err := svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	assertTransientUnavailable(t, err, ErrServiceStopped)
}

func TestServiceSubmitErrorNamesLifecycleState(t *testing.T) {
	cfg := &pgapi.Config{ActionQueueSize: 1, ActionQueueMaxSize: 1}
	cfg.InitDefaults()
	svc := NewService(zap.NewNop(), "pg", cfg, newMockRouter(), newMockTopology(), nil, nil, "local-node", nil, nil, nil)

	require.True(t, svc.submit(func() {}))
	require.False(t, svc.submit(func() {}))
	assertTransientUnavailable(t, svc.submitError(), ErrServiceNotStarted)

	_, err := svc.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, svc.Stop(context.Background()))
	for svc.submit(func() {}) {
	}
	assertTransientUnavailable(t, svc.submitError(), ErrServiceStopped)
}

func TestServiceFailedStartKeepsLifecycleState(t *testing.T) {
	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	svc := NewService(zap.NewNop(), "pg", nil, newMockRouter(), newMockTopology(), nil, bus, "local-node", nil, nil, nil)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Start(cancelled)
	require.Error(t, err)
	_, err = svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	assertTransientUnavailable(t, err, ErrServiceNotStarted)

	_, err = svc.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, svc.Stop(context.Background()))

	_, err = svc.Start(cancelled)
	require.Error(t, err)
	_, err = svc.Acquire(context.Background(), registry.ParseID("app:pg"), resource.ModeNormal)
	assertTransientUnavailable(t, err, ErrServiceStopped)
}
