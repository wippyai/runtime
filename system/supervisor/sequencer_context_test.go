// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stopBarrierControllable struct {
	started chan<- string
	release <-chan struct{}
	id      string
}

func (c *stopBarrierControllable) Start() error { return nil }
func (c *stopBarrierControllable) Stop() error {
	c.started <- c.id
	if c.release != nil {
		<-c.release
	}
	return nil
}

func TestY13PreCanceledStopStartsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := make(chan string, 1)
	sp := newSequencer(zap.NewNop())

	err := sp.processStopOperations(ctx, []operation{{
		kind: opStop, id: "service", controller: &stopBarrierControllable{id: "service", started: started},
	}})

	require.ErrorIs(t, err, context.Canceled)
	select {
	case id := <-started:
		t.Fatalf("stop unexpectedly started for %s", id)
	default:
	}
}

func TestY14StopCancellationBlocksLaterLevel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan string, 2)
	releaseFirst := make(chan struct{})
	sp := newSequencer(zap.NewNop())
	operations := []operation{
		{kind: opStop, id: "first", dependencies: []string{"later"}, controller: &stopBarrierControllable{id: "first", started: started, release: releaseFirst}},
		{kind: opStop, id: "later", controller: &stopBarrierControllable{id: "later", started: started}},
	}
	done := make(chan error, 1)
	go func() { done <- sp.processStopOperations(ctx, operations) }()

	require.Equal(t, "first", <-started)
	cancel()
	close(releaseFirst)
	require.ErrorIs(t, <-done, context.Canceled)
	select {
	case id := <-started:
		t.Fatalf("later dependency level unexpectedly started for %s", id)
	default:
	}
}

func TestY15ClosedStartStateSourceExits(t *testing.T) {
	source := make(chan struct{})
	close(source)
	target := make(chan struct{}, 1)
	doneWatching := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		forwardStartStateChanges(context.Background(), doneWatching, source, target)
		close(returned)
	}()

	<-returned
	select {
	case <-target:
		t.Fatal("closed source emitted a notification")
	default:
	}
}
