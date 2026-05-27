// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// The AMQP consumer goroutine acquires a pooled Message per delivery. If the
// handoff to the caller's channel loses to ctx cancel or lifecycle shutdown,
// the Message must be released — otherwise the pool bleeds an entry per
// dropped delivery during shutdown storms.
func TestDeliverOrRelease_ReleasesOnConsumerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lifecycle := make(chan struct{})
	deliveries := make(chan *queueapi.Delivery)

	msg := queueapi.AcquireMessage(payload.NewPayload([]byte("x"), "json"))
	msg.ID = "marker"
	delivery := &queueapi.Delivery{Message: msg}

	ok := deliverOrRelease(ctx, lifecycle, deliveries, delivery)

	assert.False(t, ok)
	assert.Empty(t, msg.ID, "ReleaseMessage clears ID; non-empty means leaked")
	assert.Nil(t, msg.Body)
}

func TestDeliverOrRelease_ReleasesOnLifecycleClose(t *testing.T) {
	ctx := context.Background()
	lifecycle := make(chan struct{})
	close(lifecycle)
	deliveries := make(chan *queueapi.Delivery)

	msg := queueapi.AcquireMessage(payload.NewPayload([]byte("x"), "json"))
	msg.ID = "marker"
	delivery := &queueapi.Delivery{Message: msg}

	ok := deliverOrRelease(ctx, lifecycle, deliveries, delivery)

	assert.False(t, ok)
	assert.Empty(t, msg.ID)
	assert.Nil(t, msg.Body)
}

func TestDeliverOrRelease_SuccessTransfersOwnership(t *testing.T) {
	ctx := context.Background()
	lifecycle := make(chan struct{})
	deliveries := make(chan *queueapi.Delivery, 1)

	msg := queueapi.AcquireMessage(payload.NewPayload([]byte("x"), "json"))
	msg.ID = "marker"
	delivery := &queueapi.Delivery{Message: msg}

	ok := deliverOrRelease(ctx, lifecycle, deliveries, delivery)

	assert.True(t, ok)
	assert.Equal(t, "marker", msg.ID)
	assert.NotNil(t, msg.Body)
}
