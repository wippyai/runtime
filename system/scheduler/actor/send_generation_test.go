// SPDX-License-Identifier: MPL-2.0
package actor

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

func TestSendRejectsReusedProcessorIdentity(t *testing.T) {
	old := pid.PID{Node: "local", Host: "actors", UniqID: "old"}
	next := pid.PID{Node: "local", Host: "actors", UniqID: "next"}
	p := &Processor{pid: next, ctx: context.Background(), queue: process.NewEventQueue()}
	p.gen.Store(p.queue.Generation())
	p.publishSignalRef() // no frame source: identity must still be published
	s := &Scheduler{}
	pkg := relay.NewPackage(old, old, "data")
	defer relay.ReleasePackage(pkg)
	if err := s.deliverToTarget(p, old, pkg); !errors.Is(err, process.ErrProcessClosed) {
		t.Fatalf("stale identity accepted: %v", err)
	}
	if p.queue.HasEvents() {
		t.Fatal("message reached reused process")
	}
	p.queue.Reset() // identity snapshot exists, but generation is now stale
	if err := s.deliverToTarget(p, next, pkg); !errors.Is(err, process.ErrProcessClosed) {
		t.Fatalf("stale generation accepted: %v", err)
	}
}
