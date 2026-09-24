// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

type affinityMessageProcess struct {
	seen   chan []string
	topics []string
}

func (*affinityMessageProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *affinityMessageProcess) Step(events []process.Event, out *process.StepOutput) error {
	for _, event := range events {
		pkg, ok := event.Data.(*relay.Package)
		if !ok {
			continue
		}
		for _, message := range pkg.Messages {
			p.topics = append(p.topics, message.Topic)
		}
	}
	if len(p.topics) == 2 {
		p.seen <- append([]string(nil), p.topics...)
		out.Done(nil)
	} else {
		out.Idle()
	}
	return nil
}
func (*affinityMessageProcess) Send(*relay.Package) error { return nil }
func (*affinityMessageProcess) Close()                    {}

// This step keeps its scheduler worker occupied without yielding to the actor scheduler.
type affinityBusyProcess struct {
	entered chan struct{}
	release atomic.Bool
	spins   atomic.Uint64
}

func (*affinityBusyProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *affinityBusyProcess) Step(_ []process.Event, out *process.StepOutput) error {
	close(p.entered)
	for !p.release.Load() {
		p.spins.Add(1)
	}
	out.Done(nil)
	return nil
}
func (*affinityBusyProcess) Send(*relay.Package) error { return nil }
func (*affinityBusyProcess) Close()                    {}

func TestAffinityWakeupRunsWhilePreviousWorkerIsBusy(t *testing.T) {
	sched := newTestScheduler(2)
	sched.Start()
	defer testStopScheduler(sched)

	// Occupy one worker so the message process and its later blocker run
	// on the same other worker. Then free the first worker for the wakeup.
	other := &resizeGateProcess{entered: make(chan struct{}), release: make(chan struct{})}
	defer releaseResizeGate(other.release)
	if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "affinity-other"}, other, "", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-other.entered:
	case <-time.After(time.Second):
		t.Fatal("first worker did not enter its step")
	}

	target := pidapi.PID{UniqID: "affinity-target"}
	receiver := &affinityMessageProcess{seen: make(chan []string, 1)}
	targetProc, err := sched.Submit(context.Background(), target, receiver, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sched.wakeAll()
	waitFor(t, func() bool { return ProcessState(targetProc.state.Load()) == StateIdle })
	affineWorker := targetProc.lastWorker.Load()

	busy := &affinityBusyProcess{entered: make(chan struct{})}
	defer busy.release.Store(true)
	busyProc, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "affinity-busy"}, busy, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sched.wakeAll()
	select {
	case <-busy.entered:
	case <-time.After(time.Second):
		t.Fatal("affine worker did not enter the non-yielding step")
	}
	if got := busyProc.lastWorker.Load(); got != affineWorker {
		t.Fatalf("blocker ran on worker %d, target affinity was %d", got, affineWorker)
	}

	releaseResizeGate(other.release)
	waitFor(t, func() bool { return sched.processorCount.Load() == 2 })
	for _, topic := range []string{"first", "second"} {
		if err := sched.Send(relay.NewPackage(pidapi.PID{}, target, topic)); err != nil {
			t.Fatal(err)
		}
	}

	select {
	case topics := <-receiver.seen:
		if len(topics) != 2 || topics[0] != "first" || topics[1] != "second" {
			t.Fatalf("message order: %v", topics)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("affine wakeup waited for a non-yielding step while another worker was idle")
	}
}
