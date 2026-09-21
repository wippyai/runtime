// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"errors"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
)

func TestRunYieldHandlerKeepsFrameUntilHandlerAndCompletionFinish(t *testing.T) {
	t.Run("asynchronous completion", func(t *testing.T) {
		ctx, frame, released := yieldFrame(t)
		started := make(chan struct{})
		complete := make(chan struct{})
		finished := make(chan error, 1)
		handler := dispatcher.HandlerFunc(func(handlerCtx context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
			go func() {
				close(started)
				<-complete
				if ctxapi.FrameFromContext(handlerCtx) == nil {
					finished <- errors.New("yield frame was released before asynchronous completion")
					return
				}
				receiver.CompleteYield(tag, true, nil)
				finished <- nil
			}()
			return nil
		})

		collector := newYieldCollector(1)
		runYieldHandler(handler, ctx, yieldFrameTestCommand{}, 1, frame, collector)
		<-started
		mustRemainLive(t, released)

		close(complete)
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		waitForYield(t, collector)
		mustRelease(t, released)
	})

	t.Run("inline completion", func(t *testing.T) {
		ctx, frame, released := yieldFrame(t)
		handler := dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
			receiver.CompleteYield(tag, true, nil)
			mustRemainLive(t, released)
			return nil
		})

		collector := newYieldCollector(1)
		runYieldHandler(handler, ctx, yieldFrameTestCommand{}, 1, frame, collector)
		waitForYield(t, collector)
		mustRelease(t, released)
	})

	t.Run("immediate error", func(t *testing.T) {
		ctx, frame, released := yieldFrame(t)
		cause := errors.New("handler failed")
		handler := dispatcher.HandlerFunc(func(context.Context, dispatcher.Command, uint64, dispatcher.ResultReceiver) error {
			return cause
		})

		collector := newYieldCollector(1)
		runYieldHandler(handler, ctx, yieldFrameTestCommand{}, 1, frame, collector)
		waitForYield(t, collector)
		mustRelease(t, released)

		events := collector.ToEvents()
		if len(events) != 1 || !errors.Is(events[0].Error, cause) {
			t.Fatalf("yield result = %#v, want handler error %v", events, cause)
		}
	})
}

func yieldFrame(t *testing.T) (context.Context, ctxapi.FrameContext, <-chan struct{}) {
	t.Helper()
	ctx, frame := ctxapi.ForkFrameContext(context.Background())
	released := make(chan struct{})
	if err := frame.Set(&ctxapi.Key{Name: "evalhost.yield_frame_test"}, ctxapi.CloserFunc(func() error {
		close(released)
		return nil
	})); err != nil {
		t.Fatalf("set frame value: %v", err)
	}
	return ctx, frame, released
}

func waitForYield(t *testing.T, collector *yieldCollector) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := collector.Wait(ctx); err != nil {
		t.Fatalf("wait for yield: %v", err)
	}
}

func mustRemainLive(t *testing.T, released <-chan struct{}) {
	t.Helper()
	select {
	case <-released:
		t.Fatal("yield frame was released too early")
	default:
	}
}

func mustRelease(t *testing.T, released <-chan struct{}) {
	t.Helper()
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("yield frame was not released")
	}
}

type yieldFrameTestCommand struct{}

func (yieldFrameTestCommand) CmdID() dispatcher.CommandID { return 1 }
