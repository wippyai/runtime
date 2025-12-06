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
	api "github.com/wippyai/runtime/api/relay"
)

// TestMailbox_MessageOrdering verifies that messages from the same source
// are delivered in FIFO order (per-sender ordering guarantee).
func TestMailbox_MessageOrdering(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  1000,
		WorkerCount: 8,
	})

	targetPID := api.PID{Host: "test", UniqID: "receiver"}
	receiverCh := make(chan *api.Package, 1000)
	_, err := mailbox.Attach(targetPID, receiverCh)
	require.NoError(t, err)

	const messagesPerSender = 100
	const numSenders = 10

	// Send messages from multiple senders concurrently
	var wg sync.WaitGroup
	for sender := 0; sender < numSenders; sender++ {
		wg.Add(1)
		go func(senderID int) {
			defer wg.Done()
			// Use unique string for each sender so hash is consistent
			sourcePID := api.PID{Host: "sender", UniqID: fmt.Sprintf("sender-%d", senderID)}
			for i := 0; i < messagesPerSender; i++ {
				pkg := &api.Package{
					Source: sourcePID,
					Target: targetPID,
					Messages: []*api.Message{{
						// Encode sequence in topic for easy parsing
						Topic: fmt.Sprintf("%d:%d", senderID, i),
					}},
				}
				err := mailbox.Send(pkg)
				assert.NoError(t, err)
			}
		}(sender)
	}

	wg.Wait()

	// Give time for delivery
	time.Sleep(50 * time.Millisecond)

	// Collect all messages
	received := make([]*api.Package, 0, numSenders*messagesPerSender)
	for {
		select {
		case pkg := <-receiverCh:
			received = append(received, pkg)
		default:
			goto done
		}
	}
done:

	require.Equal(t, numSenders*messagesPerSender, len(received),
		"should receive all messages")

	// Verify per-sender ordering: messages from same source must be in sequence
	lastSeq := make(map[string]int)
	for _, pkg := range received {
		senderKey := pkg.Source.UniqID
		if len(pkg.Messages) > 0 {
			var senderID, seq int
			_, _ = fmt.Sscanf(pkg.Messages[0].Topic, "%d:%d", &senderID, &seq)
			if prev, exists := lastSeq[senderKey]; exists {
				assert.Greater(t, seq, prev,
					"messages from sender %s out of order: got %d after %d",
					senderKey, seq, prev)
			}
			lastSeq[senderKey] = seq
		}
	}

	assert.Equal(t, numSenders, len(lastSeq), "should have received from all senders")
}

// BenchmarkMailbox_Send measures Send throughput.
func BenchmarkMailbox_Send(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  10000,
		WorkerCount: 8,
	})

	targetPID := api.PID{Host: "bench", UniqID: "target"}
	receiverCh := make(chan *api.Package, 10000)
	_, _ = mailbox.Attach(targetPID, receiverCh)

	// Drain receiver in background
	go func() {
		for range receiverCh { //nolint:revive
		}
	}()

	sourcePID := api.PID{Host: "bench", UniqID: "source"}
	pkg := &api.Package{
		Source: sourcePID,
		Target: targetPID,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = mailbox.Send(pkg)
	}
}

// BenchmarkMailbox_SendParallel measures parallel Send throughput.
func BenchmarkMailbox_SendParallel(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  100000,
		WorkerCount: 8,
	})

	targetPID := api.PID{Host: "bench", UniqID: "target"}
	receiverCh := make(chan *api.Package, 100000)
	_, _ = mailbox.Attach(targetPID, receiverCh)

	// Drain receiver in background
	go func() {
		for range receiverCh { //nolint:revive
		}
	}()

	var counter atomic.Int64

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		id := counter.Add(1)
		sourcePID := api.PID{Host: "bench", UniqID: string(rune(id))}
		pkg := &api.Package{
			Source: sourcePID,
			Target: targetPID,
		}
		for pb.Next() {
			_ = mailbox.Send(pkg)
		}
	})
}

// BenchmarkRouter_Send measures Router Send throughput.
func BenchmarkRouter_Send(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := NewNode("local")
	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  10000,
		WorkerCount: 8,
	})
	_ = node.RegisterHost("bench", mailbox)

	router := NewRouter(node, nil)

	targetPID := api.PID{Host: "bench", UniqID: "target"}
	receiverCh := make(chan *api.Package, 10000)
	_, _ = mailbox.Attach(targetPID, receiverCh)

	// Drain receiver in background
	go func() {
		for range receiverCh { //nolint:revive
		}
	}()

	sourcePID := api.PID{Host: "bench", UniqID: "source"}
	pkg := &api.Package{
		Source: sourcePID,
		Target: targetPID,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = router.Send(pkg)
	}
}

// BenchmarkRouter_SendToPeer measures Router Send to peer node throughput.
func BenchmarkRouter_SendToPeer(b *testing.B) {
	node := NewNode("local")
	router := NewRouter(node, nil)

	var received atomic.Int64
	peerReceiver := &benchReceiver{received: &received}
	_ = router.RegisterPeer("peer1", peerReceiver)

	pkg := &api.Package{
		Source: api.PID{Host: "bench", UniqID: "source"},
		Target: api.PID{Node: "peer1", Host: "bench", UniqID: "target"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = router.Send(pkg)
	}
}

type benchReceiver struct {
	received *atomic.Int64
}

func (r *benchReceiver) Send(_ *api.Package) error {
	r.received.Add(1)
	return nil
}

// BenchmarkHashString measures the hash function performance.
func BenchmarkHashString(b *testing.B) {
	testStrings := []string{
		"short",
		"medium-length-string-id",
		"very-long-unique-identifier-that-might-be-used-as-process-id",
	}

	for _, s := range testStrings {
		b.Run(s[:min(10, len(s))], func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = hashString(s)
			}
		})
	}
}

// BenchmarkBaseline_SendDirect measures direct channel send (baseline).
func BenchmarkBaseline_SendDirect(b *testing.B) {
	ch := make(chan *api.Package, 10000)

	// Drain in background
	go func() {
		for range ch { //nolint:revive
		}
	}()

	pkg := &api.Package{}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ch <- pkg
	}
}

// BenchmarkBaseline_ContextCheck measures context.Err() overhead.
func BenchmarkBaseline_ContextCheck(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ctx.Err()
	}
}

// BenchmarkBaseline_SelectSend measures select with channel send.
func BenchmarkBaseline_SelectSend(b *testing.B) {
	ctx := context.Background()
	ch := make(chan *api.Package, 10000)

	// Drain in background
	go func() {
		for range ch { //nolint:revive
		}
	}()

	pkg := &api.Package{}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		select {
		case ch <- pkg:
		case <-ctx.Done():
		}
	}
}

// BenchmarkSyncMapLoad measures sync.Map Load performance.
func BenchmarkSyncMapLoad(b *testing.B) {
	var m sync.Map
	pid := api.PID{Host: "test", UniqID: "target"}
	ch := make(chan *api.Package, 1)
	m.Store(pid, ch)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = m.Load(pid)
	}
}

// BenchmarkMailbox_SendEnqueueOnly measures just the enqueue part (no worker drain).
func BenchmarkMailbox_SendEnqueueOnly(b *testing.B) {
	ctx := context.Background()

	mailbox := NewMailbox(ctx, MailboxConfig{
		BufferSize:  b.N + 1000, // Large buffer so we don't block
		WorkerCount: 8,
	})

	// Don't attach any receiver - workers will just drop
	sourcePID := api.PID{Host: "bench", UniqID: "source"}
	targetPID := api.PID{Host: "bench", UniqID: "target"}
	pkg := &api.Package{
		Source: sourcePID,
		Target: targetPID,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = mailbox.Send(pkg)
	}
}
