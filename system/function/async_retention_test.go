// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	functionapi "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

type failingAsyncNode struct {
	err error
	mockRelayNode
	failedPackage *relay.Package
	failedMessage *relay.Message
	sends         int
}

func (n *failingAsyncNode) Send(pkg *relay.Package) error {
	n.sends++
	n.failedPackage = pkg
	n.failedMessage = pkg.Messages[0]
	return n.err
}

type discardingAsyncNode struct{ mockRelayNode }

func (*discardingAsyncNode) Send(pkg *relay.Package) error {
	relay.ReleasePackage(pkg)
	return nil
}

func TestAsyncCompletionReleasesTrackingOnDeliveryFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &failingAsyncNode{err: errors.New("caller is gone")}
		d := NewDispatcher(node, nil)
		closed := false
		reg := &mockRegistry{callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			require.NoError(t, ctxapi.FrameFromContext(ctx).Set(&ctxapi.Key{Name: "failed-send-cleanup"}, ctxapi.CloserFunc(func() error {
				closed = true
				return nil
			})))
			return &runtime.Result{Value: payload.New("completed")}, nil
		}}
		frame, fc := ctxapi.OpenFrameContext(functionapi.WithRegistry(ctxapi.NewRootContext(), reg))
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "send-failure"}))
		startAsyncForTest(frame, t, d, "function", "send-failure")
		synctest.Wait()
		require.True(t, closed)
		require.Equal(t, 1, node.sends)
		require.Empty(t, node.failedPackage.Messages, "failed delivery must release the caller-owned package")
		require.Zero(t, node.failedPackage.Target)
		require.Empty(t, node.failedMessage.Topic)
		require.Nil(t, node.failedMessage.Payloads, "rejected delivery must not retain its result payload")
		d.asyncCalls.Range(func(key, value any) bool {
			t.Errorf("completed call retained after send failure: %v (%T)", key, value)
			return true
		})
		cancelAsyncForTest(frame, t, d, "send-failure")
		synctest.Wait()
		require.Equal(t, 1, node.sends, "failed delivery must not create another outcome")
	})
}

func TestAsyncCompletionDoesNotAccumulateCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Drop delivered packages rather than retaining the fixture's results.
		d := NewDispatcher(&discardingAsyncNode{}, nil)
		reg := &mockRegistry{callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		frame, fc := ctxapi.OpenFrameContext(functionapi.WithRegistry(ctxapi.NewRootContext(), reg))
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "retention"}))
		for batch := range 10 {
			for i := range 100 {
				topic := strconv.Itoa(batch*100 + i)
				startAsyncForTest(frame, t, d, "function", topic)
				cancelAsyncForTest(frame, t, d, topic)
			}
			synctest.Wait()
			d.asyncCalls.Range(func(key, value any) bool {
				t.Errorf("completed call accumulated in batch %d: %v (%T)", batch, key, value)
				return true
			})
		}
	})
}

// Unlike dispatch-only benchmarks, this includes result delivery and waits for
// all frame cleanup before returning, so unfinished goroutines cannot pollute
// subsequent benchmark allocation measurements.
func BenchmarkNativeAsyncCompletion(b *testing.B) {
	node := &mockRelayNode{packages: make(chan *relay.Package, 1)}
	d := NewDispatcher(node, nil)
	reg := &mockRegistry{callFn: func(context.Context, runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Value: payload.New("completed")}, nil
	}}
	ctx := functionapi.WithRegistry(ctxapi.NewRootContext(), reg)
	frame, fc := ctxapi.OpenFrameContext(ctx)
	closed := make(chan struct{})
	_ = fc.Set(&ctxapi.Key{Name: "benchmark-cleanup"}, ctxapi.CloserFunc(func() error {
		close(closed)
		return nil
	}))
	_ = runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "benchmark"})
	start := functionapi.AcquireAsyncStartCmd()
	defer start.Release()
	start.Topic = "benchmark"
	receiver := &testReceiver{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = d.handleAsyncStart(frame, start, 0, receiver)
		relay.ReleasePackage(<-node.packages)
	}
	b.StopTimer()
	ctxapi.ReleaseFrameContext(fc)
	<-closed
}
