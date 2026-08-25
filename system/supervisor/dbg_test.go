package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

func TestDbgRejected(t *testing.T) {
	h := newTestHarness(t)
	h.start(context.Background())
	defer h.stop()

	noRetry := supervisor.LifecycleConfig{
		AutoStart: true, StartTimeout: 5 * time.Second, StopTimeout: 5 * time.Second,
		RetryPolicy: supervisor.RetryPolicy{MaxAttempts: 1},
	}
	register := func(id string, svc supervisor.Service) {
		h.sup.handleEvent(event.Event{System: supervisor.System, Kind: supervisor.ServiceRegister, Path: id,
			Data: &supervisor.Entry{Service: svc, Config: noRetry}})
	}
	healthy := newCountingService()
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:healthy", healthy)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
	awaitCondition(t, "healthy start", healthy.isRunning)

	stubborn := newCountingService()
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:stubborn", stubborn)
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})
	awaitCondition(t, "stubborn start", stubborn.isRunning)

	stubborn.setStopErr(errors.New("stop refused"))

	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxBegin})
	register("test:healthy", newCountingService())
	register("test:stubborn", newCountingService())
	h.sup.handleEvent(event.Event{System: registry.System, Kind: registry.TxCommit})

	time.Sleep(300 * time.Millisecond)
	hs, _ := h.sup.GetState("test:healthy")
	ss, _ := h.sup.GetState("test:stubborn")
	starts, stops := healthy.counts()
	t.Logf("healthy running=%v starts=%d stops=%d ctrlStatus=%s", healthy.isRunning(), starts, stops, hs.Status)
	t.Logf("stubborn running=%v ctrlStatus=%s", stubborn.isRunning(), ss.Status)
	for _, l := range h.logs.All() {
		t.Logf("LOG %s %v", l.Message, l.ContextMap())
	}
}
