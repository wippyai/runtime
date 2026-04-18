// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func BenchmarkDriver_Publish(b *testing.B) {
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("bench:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("bench:queue")

	memBag := attrs.NewBag()
	memBag.Set("max_length", b.N+1000)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)
	require.NoError(b, driver.DeclareQueue(ctx, queueID, cfg))

	msg := queueapi.AcquireMessage(payload.New("benchmark message"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = driver.Publish(ctx, queueID, msg)
	}
}

func BenchmarkDriver_PublishParallel(b *testing.B) {
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("bench:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("bench:queue")

	memBag := attrs.NewBag()
	memBag.Set("max_length", b.N+100000)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)
	require.NoError(b, driver.DeclareQueue(ctx, queueID, cfg))

	// Start consumer to drain the queue
	deliveries := make(chan *queueapi.Delivery, 10000)
	cancel, err := driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(b, err)
	defer cancel()

	go func() {
		for d := range deliveries {
			_ = d.Ack(ctx)
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		msg := queueapi.AcquireMessage(payload.New("benchmark message"))
		for pb.Next() {
			_ = driver.Publish(ctx, queueID, msg)
		}
	})
}

func BenchmarkDriver_PublishAndConsume(b *testing.B) {
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("bench:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("bench:queue")

	memBag := attrs.NewBag()
	memBag.Set("max_length", 10000)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)
	require.NoError(b, driver.DeclareQueue(ctx, queueID, cfg))

	deliveries := make(chan *queueapi.Delivery, 1000)
	cancel, err := driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(b, err)
	defer cancel()

	var consumed atomic.Int64
	go func() {
		for d := range deliveries {
			_ = d.Ack(ctx)
			consumed.Add(1)
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := queueapi.AcquireMessage(payload.New("benchmark message"))
		_ = driver.Publish(ctx, queueID, msg)
	}

	for consumed.Load() < int64(b.N) {
		time.Sleep(time.Microsecond)
	}
}

func BenchmarkDriver_AttachDetach(b *testing.B) {
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("bench:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("bench:queue")

	require.NoError(b, driver.DeclareQueue(ctx, queueID, &queueapi.Config{}))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deliveries := make(chan *queueapi.Delivery, 10)
		cancel, _ := driver.Attach(ctx, queueID, nil, deliveries)
		cancel()
	}
}

// Stress tests

func TestDriver_StressConcurrentPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("stress:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("stress:queue")

	memBag := attrs.NewBag()
	memBag.Set("max_length", 100000)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)
	require.NoError(t, driver.DeclareQueue(ctx, queueID, cfg))

	const numGoroutines = 100
	const messagesPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	start := make(chan struct{})

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			<-start

			for i := 0; i < messagesPerGoroutine; i++ {
				msg := queueapi.AcquireMessage(payload.New("stress test"))
				err := driver.Publish(ctx, queueID, msg)
				if err != nil {
					t.Errorf("goroutine %d: publish failed: %v", id, err)
					return
				}
			}
		}(g)
	}

	close(start)
	wg.Wait()

	info, err := driver.GetQueueInfo(ctx, queueID)
	require.NoError(t, err)
	count := info.GetInt(queueapi.StatsMessageCount, 0)
	expected := numGoroutines * messagesPerGoroutine
	if count != expected {
		t.Errorf("expected %d messages, got %d", expected, count)
	}
}

func TestDriver_StressConcurrentConsumers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("stress:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("stress:queue")

	memBag := attrs.NewBag()
	memBag.Set("max_length", 10000)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)
	require.NoError(t, driver.DeclareQueue(ctx, queueID, cfg))

	const totalMessages = 10000
	const numConsumers = 10

	for i := 0; i < totalMessages; i++ {
		msg := queueapi.AcquireMessage(payload.New("stress test"))
		require.NoError(t, driver.Publish(ctx, queueID, msg))
	}

	var consumed atomic.Int64
	var wg sync.WaitGroup
	wg.Add(numConsumers)

	consumerCtx, cancelConsumers := context.WithCancel(ctx)

	for c := 0; c < numConsumers; c++ {
		deliveries := make(chan *queueapi.Delivery, 100)
		cancel, err := driver.Attach(consumerCtx, queueID, nil, deliveries)
		require.NoError(t, err)

		go func(deliveries chan *queueapi.Delivery, cancel context.CancelFunc) {
			defer wg.Done()
			defer cancel()

			for {
				select {
				case d, ok := <-deliveries:
					if !ok {
						return
					}
					_ = d.Ack(ctx)
					consumed.Add(1)
				case <-consumerCtx.Done():
					return
				}
			}
		}(deliveries, cancel)
	}

	deadline := time.After(5 * time.Second)
	for consumed.Load() < totalMessages {
		select {
		case <-deadline:
			t.Fatalf("timeout: only consumed %d of %d messages", consumed.Load(), totalMessages)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancelConsumers()
	wg.Wait()

	if consumed.Load() != totalMessages {
		t.Errorf("expected %d consumed, got %d", totalMessages, consumed.Load())
	}
}

func TestDriver_StressPublishDuringStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("stress:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("stress:queue")

	memBag := attrs.NewBag()
	memBag.Set("max_length", 10000)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)
	require.NoError(t, driver.DeclareQueue(ctx, queueID, cfg))

	_, err := driver.Start(ctx)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)

	// Publisher goroutine that runs concurrently with Stop
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			msg := queueapi.AcquireMessage(payload.New("stress test"))
			err := driver.Publish(ctx, queueID, msg)
			if err != nil {
				// Expected: driver stopped or queue closed
				return
			}
		}
	}()

	// Let some messages get published
	time.Sleep(5 * time.Millisecond)

	// Stop while publishing is happening - should not panic or race
	err = driver.Stop(ctx)
	require.NoError(t, err)

	wg.Wait()
}

func TestDriver_StressNackRequeue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	logger := zap.NewNop()
	driver := NewDriver(registry.ParseID("stress:driver"), logger)
	ctx := context.Background()
	queueID := registry.ParseID("stress:queue")

	memBag := attrs.NewBag()
	memBag.Set("max_length", 100)
	cfg := &queueapi.Config{DriverOptions: attrs.NewBag()}
	cfg.DriverOptions.Set("memory", memBag)
	require.NoError(t, driver.DeclareQueue(ctx, queueID, cfg))

	msg := queueapi.AcquireMessage(payload.New("requeue test"))
	msg.ID = "requeue-msg"
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	deliveries := make(chan *queueapi.Delivery, 10)
	cancel, err := driver.Attach(ctx, queueID, nil, deliveries)
	require.NoError(t, err)
	defer cancel()

	const nackCount = 50
	for i := 0; i < nackCount; i++ {
		select {
		case d := <-deliveries:
			if d.Message.ID != "requeue-msg" {
				t.Fatalf("unexpected message ID: %s", d.Message.ID)
			}
			err := d.Nack(ctx)
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for message on iteration %d", i)
		}
	}

	select {
	case d := <-deliveries:
		require.NoError(t, d.Ack(ctx))
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for final message")
	}
}
