// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	consumerapi "github.com/wippyai/runtime/api/service/queue/consumer"
	"go.uber.org/zap"
)

// When a handler has already settled the delivery (e.g. Lua called
// msg:ack() explicitly), the consumer's post-handler auto-ack must not
// fire a second Ack at the broker. AMQP raises PRECONDITION_FAILED on
// double-ack and SQS DeleteMessage twice races with visibility-timeout
// redelivery, so this is a contract test for the gate.
func TestConsumer_SkipsAutoAck_WhenHandlerAlreadySettled(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	var ackCalls, nackCalls atomic.Int32
	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "pre-acked",
			Body:    payload.New("test"),
			Headers: attrs.NewBag(),
		},
		Ack: func(_ context.Context) error {
			ackCalls.Add(1)
			return nil
		},
		Nack: func(_ context.Context) error {
			nackCalls.Add(1)
			return nil
		},
	}

	// Simulate the Lua binding's manual ack path: claim the settle
	// slot, then invoke Ack. The consumer must see Settled()==true and
	// skip its own post-handler auto-ack.
	funcReg := &mockFuncRegistry{
		onCall: func() {
			if delivery.MarkSettled() {
				_ = delivery.Ack(ctx)
			}
		},
	}

	c := NewConsumer(
		registry.NewID("test", "consumer"),
		&consumerapi.Config{
			ConsumerOptions: queueapi.ConsumerOptions{
				Queue:       registry.NewID("test", "queue"),
				Func:        registry.NewID("test", "func"),
				Concurrency: 1,
				Prefetch:    10,
			},
		},
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := c.Start(ctx)
	require.NoError(t, err)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Stop(stopCtx)
	}()

	driver.deliveries <- delivery

	assert.Eventually(t, func() bool { return ackCalls.Load() == 1 },
		2*time.Second, 10*time.Millisecond,
		"handler's manual Ack must fire exactly once")
	assert.Equal(t, int32(0), nackCalls.Load(),
		"handler's success path must not produce any Nack")

	// Give the consumer's defer a beat to (wrongly) re-ack — if the
	// skip-when-settled gate regresses, ackCalls will grow past 1.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), ackCalls.Load(),
		"consumer must not double-ack after handler already settled")
}

// The mirror case: handler nacks via the wrapper, consumer must not
// then emit an auto-ack on the error-free return path and must not
// double-nack on the error path.
func TestConsumer_SkipsAutoAck_WhenHandlerAlreadyNacked(t *testing.T) {
	ctx := context.Background()
	driver := &mockDriver{}

	var ackCalls, nackCalls atomic.Int32
	delivery := &queueapi.Delivery{
		Message: &queueapi.Message{
			ID:      "pre-nacked",
			Body:    payload.New("test"),
			Headers: attrs.NewBag(),
		},
		Ack: func(_ context.Context) error {
			ackCalls.Add(1)
			return nil
		},
		Nack: func(_ context.Context) error {
			nackCalls.Add(1)
			return nil
		},
	}

	funcReg := &mockFuncRegistry{
		onCall: func() {
			if delivery.MarkSettled() {
				_ = delivery.Nack(ctx)
			}
		},
	}

	c := NewConsumer(
		registry.NewID("test", "consumer"),
		&consumerapi.Config{
			ConsumerOptions: queueapi.ConsumerOptions{
				Queue:       registry.NewID("test", "queue"),
				Func:        registry.NewID("test", "func"),
				Concurrency: 1,
				Prefetch:    10,
			},
		},
		driver,
		funcReg,
		zap.NewNop(),
	)

	_, err := c.Start(ctx)
	require.NoError(t, err)
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Stop(stopCtx)
	}()

	driver.deliveries <- delivery

	assert.Eventually(t, func() bool { return nackCalls.Load() == 1 },
		2*time.Second, 10*time.Millisecond,
		"handler's manual Nack must fire exactly once")

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), ackCalls.Load(),
		"consumer must not auto-ack when handler already nacked")
	assert.Equal(t, int32(1), nackCalls.Load(),
		"consumer must not double-nack when handler already nacked")
}
