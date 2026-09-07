// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

type mailboxReleaseProbe struct{ releases atomic.Int32 }

func (p *mailboxReleaseProbe) Release() { p.releases.Add(1) }

func probedPackage(target pidapi.PID) (*relay.Package, *mailboxReleaseProbe) {
	probe := &mailboxReleaseProbe{}
	msg := relay.AcquireMessage()
	msg.Topic = "probe"
	msg.SetRetentionLease(probe)
	return relay.NewMessagePackage(pidapi.PID{}, target, msg), probe
}

func TestMailbox_NewMailbox(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	t.Run("default configuration", func(t *testing.T) {
		mailbox := NewMailbox(ctx,
			WithBufferSize(100),
			WithWorkerCount(4),
			WithLogger(logger),
		)
		assert.NotNil(t, mailbox)
	})

	t.Run("custom configuration", func(t *testing.T) {
		mailbox := NewMailbox(ctx,
			WithBufferSize(1000),
			WithWorkerCount(8),
			WithLogger(logger),
		)
		assert.NotNil(t, mailbox)
	})
}

func TestMailbox_Attach(t *testing.T) {
	ctx := context.Background()
	mailbox := NewMailbox(ctx,
		WithBufferSize(100),
		WithWorkerCount(4),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// First attachment
	ch1 := make(chan *relay.Package, 10)
	cancel1, err1 := mailbox.Attach(pid, ch1)
	assert.NoError(t, err1)
	assert.NotNil(t, cancel1)

	// Try duplicate attachment
	ch2 := make(chan *relay.Package, 10)
	_, err2 := mailbox.Attach(pid, ch2)
	assert.Error(t, err2)
	assert.ErrorIs(t, err2, ErrAlreadyAttached)

	// Test cancellation
	cancel1()
	time.Sleep(time.Millisecond * 10) // Allow time for the delete operation
	_, exists := mailbox.receivers.Load(pid.String())
	assert.False(t, exists)
}

func TestMailbox_Send(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(2),
		WithWorkerCount(1),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test", Payloads: nil},
		},
	}

	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMailbox_SendCancelledContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(1),
		WithWorkerCount(0), // no workers so jobCh is never drained
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Pre-fill the job channel
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "dummy"},
		},
	}
	err := mailbox.Send(pkg)
	assert.NoError(t, err)
}

func TestMailbox_SendContextHonorsCallerCancellation(t *testing.T) {
	mailbox := NewMailbox(context.Background(),
		WithBufferSize(1),
		WithWorkerCount(0), // keep the queue full for the cancellation check
	)

	target := pidapi.PID{Host: "host1", UniqID: "uniq1"}
	pkg := &relay.Package{Target: target}
	require.NoError(t, mailbox.Send(pkg))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := &relay.Package{Target: target}
	err := mailbox.SendContext(ctx, blocked)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestMailbox_NoReceiver(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(2),
		WithWorkerCount(1),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// send message without attaching a receiver
	pkg, probe := probedPackage(pid)
	err := mailbox.Send(pkg)
	assert.NoError(t, err) // send should succeed even without receiver
	assert.Eventually(t, func() bool { return probe.releases.Load() == 1 }, time.Second, time.Millisecond,
		"accepted package without a receiver must be released by the mailbox")
}

func TestMailbox_DetachReleasesBufferedPackages(t *testing.T) {
	mailbox := NewMailbox(context.Background(), WithBufferSize(4), WithWorkerCount(1))
	target := pidapi.PID{Node: "node1", Host: "host1", UniqID: "detach"}
	receiverCh := make(chan *relay.Package, 4)
	_, err := mailbox.Attach(target, receiverCh)
	require.NoError(t, err)

	pkg, probe := probedPackage(target)
	require.NoError(t, mailbox.Send(pkg))
	mailbox.Detach(target)

	assert.Eventually(t, func() bool { return probe.releases.Load() == 1 }, time.Second, time.Millisecond,
		"detaching a receiver must release packages already accepted by its channel")
}

func TestMailbox_DetachReattachDoesNotCrossReceiverGeneration(t *testing.T) {
	mailbox := NewMailbox(context.Background(), WithBufferSize(8), WithWorkerCount(1))
	target := pidapi.PID{Node: "node1", Host: "host1", UniqID: "generation"}
	oldCh := make(chan *relay.Package, 8)
	_, err := mailbox.Attach(target, oldCh)
	require.NoError(t, err)

	oldPkg, oldProbe := probedPackage(target)
	require.NoError(t, mailbox.Send(oldPkg))
	mailbox.Detach(target)
	require.Eventually(t, func() bool { return oldProbe.releases.Load() == 1 }, time.Second, time.Millisecond)
	select {
	case <-oldCh:
		t.Fatal("detached receiver retained a package")
	default:
	}

	newCh := make(chan *relay.Package, 1)
	_, err = mailbox.Attach(target, newCh)
	require.NoError(t, err)
	newPkg, newProbe := probedPackage(target)
	require.NoError(t, mailbox.Send(newPkg))
	select {
	case delivered := <-newCh:
		relay.ReleasePackage(delivered)
	case <-time.After(time.Second):
		t.Fatal("reattached receiver did not receive its package")
	}
	require.Equal(t, int32(1), newProbe.releases.Load())
	select {
	case <-oldCh:
		t.Fatal("old receiver received a package after reattach")
	default:
	}
	mailbox.Detach(target)
}

func TestMailbox_DetachReattachConcurrentStress(t *testing.T) {
	mailbox := NewMailbox(context.Background(), WithBufferSize(32), WithWorkerCount(2))
	target := pidapi.PID{Node: "node1", Host: "host1", UniqID: "stress"}

	for round := 0; round < 100; round++ {
		oldCh := make(chan *relay.Package, 32)
		_, err := mailbox.Attach(target, oldCh)
		require.NoError(t, err)
		probes := make([]*mailboxReleaseProbe, 8)
		var sends sync.WaitGroup
		for i := range probes {
			pkg, probe := probedPackage(target)
			probes[i] = probe
			sends.Add(1)
			go func(pkg *relay.Package) {
				defer sends.Done()
				_ = mailbox.Send(pkg)
			}(pkg)
		}
		mailbox.Detach(target)
		sends.Wait()
		for _, probe := range probes {
			require.Eventually(t, func() bool { return probe.releases.Load() == 1 }, time.Second, time.Millisecond)
		}
		newCh := make(chan *relay.Package, 1)
		_, err = mailbox.Attach(target, newCh)
		require.NoError(t, err)
		mailbox.Detach(target)
	}
}

func TestMailbox_DetachDoesNotWaitForBlockedAdmission(t *testing.T) {
	mailbox := NewMailbox(context.Background(), WithBufferSize(1), WithWorkerCount(1))
	target := pidapi.PID{Node: "node1", Host: "host1", UniqID: "blocked-admission"}
	_, err := mailbox.Attach(target, make(chan *relay.Package))
	require.NoError(t, err)

	packages := make([]*relay.Package, 3)
	probes := make([]*mailboxReleaseProbe, 3)
	for i := range packages {
		packages[i], probes[i] = probedPackage(target)
	}
	// The first job blocks in the unbuffered receiver; the second fills the
	// mailbox queue, leaving the third sender blocked on admission.
	require.NoError(t, mailbox.Send(packages[0]))
	require.NoError(t, mailbox.Send(packages[1]))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- mailbox.SendContext(ctx, packages[2]) }()
	require.Eventually(t, func() bool {
		return len(mailbox.jobQueues[0]) == 1
	}, time.Second, time.Millisecond, "second package did not remain queued behind blocked delivery")

	detached := make(chan struct{})
	go func() {
		mailbox.Detach(target)
		close(detached)
	}()
	select {
	case <-detached:
	case <-time.After(time.Second):
		t.Fatal("Detach waited on a blocked queue admission")
	}
	if err := <-errCh; err != nil {
		relay.ReleasePackage(packages[2])
	}
	for _, probe := range probes {
		require.Eventually(t, func() bool { return probe.releases.Load() == 1 }, time.Second, time.Millisecond)
	}
}

func TestMailbox_ShutdownReleasesQueuedPackages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mailbox := NewMailbox(ctx, WithBufferSize(8), WithWorkerCount(1))
	target := pidapi.PID{Node: "node1", Host: "host1", UniqID: "shutdown"}
	// Keep the worker from handing packages to a receiver; shutdown must own
	// and release packages remaining in its internal queue.
	packages := make([]*relay.Package, 8)
	probes := make([]*mailboxReleaseProbe, 8)
	for i := range packages {
		packages[i], probes[i] = probedPackage(target)
		require.NoError(t, mailbox.Send(packages[i]))
	}
	cancel()
	assert.Eventually(t, func() bool {
		for _, probe := range probes {
			if probe.releases.Load() != 1 {
				return false
			}
		}
		return true
	}, time.Second, time.Millisecond, "mailbox shutdown leaked queued packages")
}

func TestMailbox_DetachDuringDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(2),
		WithWorkerCount(1),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	// Create a blocked receiver
	receiverCh := make(chan *relay.Package)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// send message
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test"},
		},
	}
	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	// Detach receiver during delivery attempt
	mailbox.Detach(pid)

	// Allow some time for message processing
	time.Sleep(time.Millisecond * 100)
	// Message should be dropped without error
}

// A receiver's owner closes its own channel on teardown. A delivery racing that
// close hits a send-on-closed-channel, which must be dropped, never panicking
// the worker — proven by a subsequent delivery to a live receiver on the same
// worker still arriving.
func TestMailbox_ClosedReceiverDoesNotKillWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx, WithBufferSize(4), WithWorkerCount(1))

	dead := pidapi.PID{Node: "node1", Host: "host1", UniqID: "dead"}
	deadCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(dead, deadCh)
	assert.NoError(t, err)
	close(deadCh) // owner closed its channel on teardown

	live := pidapi.PID{Node: "node1", Host: "host1", UniqID: "live"}
	liveCh := make(chan *relay.Package, 1)
	_, err = mailbox.Attach(live, liveCh)
	assert.NoError(t, err)

	src := pidapi.PID{Node: "node1", Host: "host1", UniqID: "src"}
	assert.NoError(t, mailbox.Send(&relay.Package{Source: src, Target: dead,
		Messages: []*relay.Message{{Topic: "x"}}}))
	assert.NoError(t, mailbox.Send(&relay.Package{Source: src, Target: live,
		Messages: []*relay.Message{{Topic: "y"}}}))

	select {
	case got := <-liveCh:
		assert.Equal(t, live, got.Target)
	case <-time.After(2 * time.Second):
		t.Fatal("worker died after a send to a closed receiver: live delivery never arrived")
	}
}

func TestMailbox_MultipleWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(100),
		WithWorkerCount(4), // Multiple workers
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 100)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// send multiple messages concurrently
	const messageCount = 50
	errorCh := make(chan error, messageCount)
	receivedCount := 0

	// send messages
	for i := 0; i < messageCount; i++ {
		go func(i int) {
			pkg := &relay.Package{
				Target: pid,
				Messages: []*relay.Message{
					{Topic: fmt.Sprintf("test-%d", i)},
				},
			}
			errorCh <- mailbox.Send(pkg)
		}(i)
	}

	// Collect errors
	for i := 0; i < messageCount; i++ {
		err := <-errorCh
		assert.NoError(t, err)
	}

	// Collect received messages
	timeout := time.After(time.Second)
	for receivedCount < messageCount {
		select {
		case <-receiverCh:
			receivedCount++
		case <-timeout:
			t.Fatalf("timeout waiting for messages, received %d/%d", receivedCount, messageCount)
			return
		}
	}

	assert.Equal(t, messageCount, receivedCount)
}

func TestMailbox_Shutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(2),
		WithWorkerCount(1),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// First ensure sending works before shutdown
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test"},
		},
	}
	err = mailbox.Send(pkg)
	assert.NoError(t, err)
	select {
	case delivered := <-receiverCh:
		relay.ReleasePackage(delivered)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for pre-shutdown delivery")
	}

	// Now cancel the mailbox context
	cancel()
	time.Sleep(time.Millisecond * 10) // Give workers time to shut down

	// Try to send a message after shutdown
	err = mailbox.Send(pkg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestMailbox_InvalidConfig(t *testing.T) {
	ctx := context.Background()

	t.Run("zero buffer size is allowed", func(t *testing.T) {
		mailbox := NewMailbox(ctx,
			WithBufferSize(0),
			WithWorkerCount(4),
		)
		assert.NotNil(t, mailbox)
	})

	t.Run("negative buffer size panics", func(t *testing.T) {
		assert.Panics(t, func() {
			NewMailbox(ctx,
				WithBufferSize(-1),
				WithWorkerCount(4),
			)
		})
	})

	t.Run("zero worker count defaults to 1", func(t *testing.T) {
		mailbox := NewMailbox(ctx,
			WithBufferSize(100),
			WithWorkerCount(0),
		)
		assert.NotNil(t, mailbox)
		assert.Equal(t, 1, mailbox.config.workerCount)
	})

	t.Run("negative worker count defaults to 1", func(t *testing.T) {
		mailbox := NewMailbox(ctx,
			WithBufferSize(100),
			WithWorkerCount(-1),
		)
		assert.NotNil(t, mailbox)
		assert.Equal(t, 1, mailbox.config.workerCount)
	})

	t.Run("nil logger defaults to noop", func(t *testing.T) {
		mailbox := NewMailbox(ctx,
			WithBufferSize(100),
			WithWorkerCount(4),
		)
		assert.NotNil(t, mailbox)
		assert.NotNil(t, mailbox.config.logger)
	})
}

func TestMailbox_SendMultipleMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(100),
		WithWorkerCount(1),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// Create a package with multiple messages
	pkg := &relay.Package{
		Target: pid,
		Messages: []*relay.Message{
			{Topic: "test1", Payloads: nil},
			{Topic: "test2", Payloads: nil},
			{Topic: "test3", Payloads: nil},
		},
	}

	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Len(t, received.Messages, 3)
		assert.Equal(t, "test1", received.Messages[0].Topic)
		assert.Equal(t, "test2", received.Messages[1].Topic)
		assert.Equal(t, "test3", received.Messages[2].Topic)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMailbox_SendEmptyMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(100),
		WithWorkerCount(1),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// Create a package with empty messages array
	pkg := &relay.Package{
		Target:   pid,
		Messages: []*relay.Message{},
	}

	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Empty(t, received.Messages)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMailbox_SendNilMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(100),
		WithWorkerCount(1),
	)

	pid := pidapi.PID{
		Node:   "node1",
		Host:   "host1",
		UniqID: "uniq1",
	}

	receiverCh := make(chan *relay.Package, 1)
	_, err := mailbox.Attach(pid, receiverCh)
	assert.NoError(t, err)

	// Create a package with nil messages array
	pkg := &relay.Package{
		Target:   pid,
		Messages: nil,
	}

	err = mailbox.Send(pkg)
	assert.NoError(t, err)

	select {
	case received := <-receiverCh:
		assert.Equal(t, pkg, received)
		assert.Nil(t, received.Messages)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestMailbox_SendNilPackage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	mailbox := NewMailbox(ctx,
		WithBufferSize(100),
		WithWorkerCount(1),
	)

	// Try to send nil package - this should return an error
	err := mailbox.Send(nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNilPackage)
}
