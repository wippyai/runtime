// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// Queue-side DriverOptions must use "default_visibility_timeout" to avoid
// collision with the consumer-side "visibility_timeout" (per-receive
// override). Reading the queue-side key from buildQueueAttributes confirms
// the two scopes stay separated.
func TestBuildQueueAttributes_QueueDefaultVisibilityTimeout(t *testing.T) {
	d := &Driver{cfg: mustSQSCfg()}
	drv := attrs.NewBag()
	drv.Set(optionQueueDefaultVisibilityTimeout, 120)
	root := attrs.NewBag()
	root.Set("sqs", drv)

	got := d.buildQueueAttributes("q", &queueapi.Config{DriverOptions: root})
	if assert.NotNil(t, got) {
		assert.Equal(t, "120", got["VisibilityTimeout"],
			"queue-side default_visibility_timeout should populate VisibilityTimeout")
	}
}

// The legacy "visibility_timeout" key on the queue-side must no longer take
// effect — it now means "consumer-side per-receive override" exclusively.
func TestBuildQueueAttributes_IgnoresConsumerKeyOnQueueSide(t *testing.T) {
	d := &Driver{cfg: mustSQSCfg()}
	drv := attrs.NewBag()
	drv.Set("visibility_timeout", 120) // consumer-side key, misplaced on queue
	root := attrs.NewBag()
	root.Set("sqs", drv)

	got := d.buildQueueAttributes("q", &queueapi.Config{DriverOptions: root})
	if got != nil {
		_, present := got["VisibilityTimeout"]
		assert.False(t, present, "consumer key must not seed queue attrs")
	}
}

// The consumer-side "visibility_timeout" still drives per-receive overrides.
// Guard against accidental rename on the consumer path.
func TestResolveReceiveParams_ConsumerVisibilityTimeout(t *testing.T) {
	drv := attrs.NewBag()
	drv.Set("visibility_timeout", 45)
	opts := &queueapi.ConsumerOptions{
		DriverOptions: attrs.NewBag(),
	}
	opts.DriverOptions.Set("sqs", drv)

	_, _, vis := resolveReceiveParams(opts)
	assert.Equal(t, int32(45), vis)
}
