// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func lookupWaitContext(t *testing.T) (context.Context, *eventual.Service) {
	t.Helper()
	svc := eventual.NewService(eventual.Config{LocalNodeID: "node-A"})
	require.NoError(t, svc.Start(context.Background()))
	t.Cleanup(func() { _ = svc.Stop() })
	ctx := ctxapi.NewRootContext()
	topology.WithEventualRegistry(ctx, svc)
	return ctx, svc
}

func TestDispatcher_LookupWaitCompletesWhenTheNameIsBound(t *testing.T) {
	ctx, svc := lookupWaitContext(t)
	d := NewDispatcher(nil, nil, nil, nil)
	receiver := &asyncResultReceiver{done: make(chan struct{})}

	require.NoError(t, d.handleLookupWait(ctx, &process.LookupWaitCmd{Name: "svc.later", Timeout: 5 * time.Second}, 1, receiver))
	require.Eventually(t, func() bool { return svc.Waiting() == 1 }, time.Second, time.Millisecond)

	p := pid.PID{Node: "node-A", Host: "workers", UniqID: "sup"}
	_, err := svc.Register("svc.later", p)
	require.NoError(t, err)
	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("lookup wait did not complete when the name was bound")
	}
	require.NoError(t, receiver.err)
	require.Equal(t, process.LookupWaitResult{PID: p, Found: true}, receiver.data)
}

func TestDispatcher_LookupWaitReportsNotFoundAtTheTimeout(t *testing.T) {
	ctx, svc := lookupWaitContext(t)
	d := NewDispatcher(nil, nil, nil, nil)
	receiver := &asyncResultReceiver{done: make(chan struct{})}

	require.NoError(t, d.handleLookupWait(ctx, &process.LookupWaitCmd{Name: "svc.never", Timeout: 30 * time.Millisecond}, 1, receiver))
	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("lookup wait did not end at its timeout")
	}
	require.NoError(t, receiver.err)
	require.Equal(t, process.LookupWaitResult{}, receiver.data)
	require.Zero(t, svc.Waiting())
}

func TestDispatcher_LookupWaitEndsWithTheWaitingProcess(t *testing.T) {
	root, svc := lookupWaitContext(t)
	ctx, cancel := context.WithCancel(root)
	d := NewDispatcher(nil, nil, nil, nil)
	receiver := &asyncResultReceiver{done: make(chan struct{})}

	require.NoError(t, d.handleLookupWait(ctx, &process.LookupWaitCmd{Name: "svc.never", Timeout: time.Minute}, 1, receiver))
	require.Eventually(t, func() bool { return svc.Waiting() == 1 }, time.Second, time.Millisecond)
	cancel()
	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("lookup wait outlived its process")
	}
	require.ErrorIs(t, receiver.err, context.Canceled)
	require.Zero(t, svc.Waiting())
}

func TestDispatcher_LookupWaitWithoutAnEventualRegistryIsUnavailable(t *testing.T) {
	d := NewDispatcher(nil, nil, nil, nil)
	receiver := &mockResultReceiver{}
	require.NoError(t, d.handleLookupWait(ctxapi.NewRootContext(), &process.LookupWaitCmd{Name: "svc", Timeout: time.Second}, 1, receiver))
	require.ErrorIs(t, receiver.err, topology.ErrNameRegistryUnavailable)
}
