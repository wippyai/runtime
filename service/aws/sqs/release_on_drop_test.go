// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// When the consumer ctx is already cancelled, handing off a pooled Delivery
// must release the Message back to the pool rather than leak it. Readers of
// the pool rely on Body/ID being cleared after ReleaseMessage — assert those
// fields are zeroed as a proxy for "was released".
func TestDeliverOrRelease_ReleasesOnConsumerCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so the send never wins

	lifecycle := make(chan struct{})
	deliveries := make(chan *queueapi.Delivery) // unbuffered — send would block

	msg := queueapi.AcquireMessage(payload.NewPayload([]byte("x"), "json"))
	msg.ID = "marker"
	delivery := &queueapi.Delivery{Message: msg}

	ok := deliverOrRelease(ctx, lifecycle, deliveries, delivery)

	assert.False(t, ok, "should report drop when ctx cancelled before send")
	assert.Empty(t, msg.ID, "ReleaseMessage clears ID; non-empty means leaked")
	assert.Nil(t, msg.Body, "ReleaseMessage clears Body; non-nil means leaked")
}

// Mirror the above for the lifecycle channel path. Closing lifecycle should
// trigger release just like consumer cancel does.
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

// Happy path: when the send succeeds, the helper must NOT release the
// message — ownership transfers to the consumer.
func TestDeliverOrRelease_SuccessTransfersOwnership(t *testing.T) {
	ctx := context.Background()
	lifecycle := make(chan struct{})
	deliveries := make(chan *queueapi.Delivery, 1)

	msg := queueapi.AcquireMessage(payload.NewPayload([]byte("x"), "json"))
	msg.ID = "marker"
	delivery := &queueapi.Delivery{Message: msg}

	ok := deliverOrRelease(ctx, lifecycle, deliveries, delivery)

	assert.True(t, ok)
	assert.Equal(t, "marker", msg.ID, "ownership transferred; helper must not release")
	assert.NotNil(t, msg.Body)
}
