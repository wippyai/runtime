// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

// Delivery.Ack / Delivery.Nack take a ctx so callers can signal that
// the settle should be abandoned — typically a Lua handler whose
// deadline already fired, or a consumer worker that is shutting down.
// The amqp091 Ack/Nack calls are not ctx-aware, so the closure must
// honor cancellation itself: if ctx is already done, no broker frame
// is emitted and the caller sees the context error back.
func TestAMQPDriver_DeliveryAck_HonorsCancelledCtx(t *testing.T) {
	driver := setupDriver(t)
	ctx := context.Background()

	queueID := registry.NewID("test", "ack-ctx-"+time.Now().Format("150405.000"))
	require.NoError(t, driver.DeclareQueue(ctx, queueID, &queueapi.Config{QueueName: queueID.Name}))

	deliveries := make(chan *queueapi.Delivery, 2)
	cancelAttach, err := driver.Attach(ctx, queueID, &queueapi.ConsumerOptions{}, deliveries)
	require.NoError(t, err)
	t.Cleanup(cancelAttach)

	msg := queueapi.AcquireMessage(payload.New("hi"))
	msg.ID = "ack-ctx"
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	var delivery *queueapi.Delivery
	select {
	case delivery = <-deliveries:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never arrived")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	err = delivery.Ack(cancelled)
	require.Error(t, err, "Ack must refuse to settle when ctx is already cancelled")
	require.True(t, errors.Is(err, context.Canceled),
		"Ack must surface the caller's ctx error verbatim, got: %v", err)

	// Sanity: a fresh, live ctx must still settle the delivery so the
	// refusal above is not a blanket pre-check regression.
	require.NoError(t, delivery.Ack(ctx))
}

// Mirror the Nack path — same contract, separate closure in the driver.
func TestAMQPDriver_DeliveryNack_HonorsCancelledCtx(t *testing.T) {
	driver := setupDriver(t)
	ctx := context.Background()

	queueID := registry.NewID("test", "nack-ctx-"+time.Now().Format("150405.000"))
	require.NoError(t, driver.DeclareQueue(ctx, queueID, &queueapi.Config{QueueName: queueID.Name}))

	deliveries := make(chan *queueapi.Delivery, 2)
	cancelAttach, err := driver.Attach(ctx, queueID, &queueapi.ConsumerOptions{}, deliveries)
	require.NoError(t, err)
	t.Cleanup(cancelAttach)

	msg := queueapi.AcquireMessage(payload.New("hi"))
	msg.ID = "nack-ctx"
	require.NoError(t, driver.Publish(ctx, queueID, msg))

	var delivery *queueapi.Delivery
	select {
	case delivery = <-deliveries:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery never arrived")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	err = delivery.Nack(cancelled)
	require.Error(t, err, "Nack must refuse to settle when ctx is already cancelled")
	require.True(t, errors.Is(err, context.Canceled),
		"Nack must surface the caller's ctx error verbatim, got: %v", err)
}
