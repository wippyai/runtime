// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	temporalclient "go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

type linearizationTemporalClient struct {
	temporalclient.Client
	closeCalls atomic.Int32
	closeOnce  sync.Once
	closeHit   chan struct{}
	closeGate  chan struct{}
}

func (c *linearizationTemporalClient) Close() {
	c.closeCalls.Add(1)
	c.closeOnce.Do(func() { close(c.closeHit) })
	<-c.closeGate
}

func newLinearizationClient() (*Client, *linearizationTemporalClient) {
	underlying := &linearizationTemporalClient{
		closeHit:  make(chan struct{}),
		closeGate: make(chan struct{}),
	}
	wrapped := NewClient(
		registry.NewID("test", "linearization"),
		zap.NewNop(),
		underlying,
		&api.ClientConfig{},
	)
	return wrapped, underlying
}

func waitForTemporalShutdown(t *testing.T, c *Client) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for !c.closed.Load() {
		select {
		case <-timer.C:
			t.Fatal("Stop did not mark the client closed")
		default:
			runtime.Gosched()
		}
	}
}

func TestN07TemporalAcceptedAcquireDelaysStop(t *testing.T) {
	wrapped, underlying := newLinearizationClient()
	lease, err := wrapped.Acquire(context.Background(), registry.ID{}, resource.ModeNormal)
	require.NoError(t, err)

	stopDone := make(chan error, 1)
	go func() { stopDone <- wrapped.Stop(context.Background()) }()
	waitForTemporalShutdown(t, wrapped)

	select {
	case <-underlying.closeHit:
		t.Fatal("Stop closed the Temporal client while an accepted lease was held")
	default:
	}

	close(underlying.closeGate)
	lease.Release()
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop did not complete after lease release")
	}
	require.Equal(t, int32(1), underlying.closeCalls.Load())
}

func TestN08TemporalShutdownWinsAcquire(t *testing.T) {
	wrapped, underlying := newLinearizationClient()
	stopDone := make(chan error, 1)
	go func() { stopDone <- wrapped.Stop(context.Background()) }()

	select {
	case <-underlying.closeHit:
	case <-time.After(time.Second):
		t.Fatal("Stop did not reach client close")
	}

	lease, err := wrapped.Acquire(context.Background(), registry.ID{}, resource.ModeNormal)
	require.ErrorContains(t, err, "closed")
	require.Nil(t, lease)

	close(underlying.closeGate)
	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop did not complete")
	}
	require.Equal(t, int32(1), underlying.closeCalls.Load())
}
