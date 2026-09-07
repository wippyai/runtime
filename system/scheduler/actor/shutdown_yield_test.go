// SPDX-License-Identifier: MPL-2.0
package actor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

type shutdownGateHandler struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *shutdownGateHandler) Handle(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	h.once.Do(func() { close(h.started) })
	go func() { <-h.release; receiver.CompleteYield(tag, nil, nil) }()
	return nil
}

func TestShutdownResumesAsyncCleanup(t *testing.T) {
	handler := &shutdownGateHandler{started: make(chan struct{}), release: make(chan struct{})}
	registry := scheduler.NewRegistry()
	registry.Register(1, handler)
	results := make(chan *runtime.Result, 1)
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(&testLifecycle{onComplete: func(_ context.Context, _ pidapi.PID, result *runtime.Result) { results <- result }}))
	sched.Start()
	_, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "shutdown-yield"}, &slowProcess{}, "", nil)
	require.NoError(t, err)
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		t.Fatal("process did not yield")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	go func() { sched.Stop(ctx); close(stopped) }()
	require.Eventually(t, func() bool { return sched.isStopping() }, time.Second, time.Millisecond)
	// Give the worker time to run out of immediate work during shutdown.
	time.Sleep(30 * time.Millisecond)
	close(handler.release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		cancel()
		<-stopped
		t.Fatal("shutdown abandoned async cleanup until its timeout")
	}
	completed := <-results
	require.NoError(t, completed.Error)
}
