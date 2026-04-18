// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	apierror "github.com/wippyai/runtime/api/error"
	queueapi "github.com/wippyai/runtime/api/queue"
	sqsapi "github.com/wippyai/runtime/api/service/aws/sqs"
)

func mustSQSCfg() *sqsapi.Config {
	c := &sqsapi.Config{}
	c.InitDefaults()
	return c
}

// AWS identifies FIFO queues by a ".fifo" suffix on the name, and refuses
// CreateQueue without FifoQueue=true set on the attributes map. Declaring an
// "*.fifo" queue must therefore add the attribute automatically.
func TestIsFIFOName(t *testing.T) {
	cases := map[string]bool{
		"orders.fifo":     true,
		"test.queue.fifo": true,
		"orders":          false,
		"fifo":            false, // must end with .fifo, not equal
		"":                false,
		"with-dash.fifo":  true,
		"dots.in.it.fifo": true,
		"orders.fifo.bak": false,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, want, isFIFOName(name))
		})
	}
}

// FIFO queues require MessageGroupId on every publish. Validating early (in
// applyHeaders / Publish) gives callers a clean apierror instead of an opaque
// AWS MissingParameter response.
func TestValidateFIFOPublish_RequiresMessageGroupID(t *testing.T) {
	headers := map[string]any{
		"correlation_id": "x",
	}
	err := validateFIFOPublish(headers)
	if assert.Error(t, err, "must reject FIFO publish without message_group_id") {
		var re apierror.Error
		assert.ErrorAs(t, err, &re)
		assert.Equal(t, apierror.Invalid, re.Kind())
		assert.Contains(t, strings.ToLower(err.Error()), "message_group_id")
	}
}

func TestValidateFIFOPublish_AcceptsGroupID(t *testing.T) {
	headers := map[string]any{publishMessageGroup: "tenant-1"}
	assert.NoError(t, validateFIFOPublish(headers))
}

// buildQueueAttributes must set FifoQueue=true for .fifo names so CreateQueue
// doesn't reject the attributes as inconsistent with the queue name.
func TestBuildQueueAttributes_FIFOSetsFifoQueueFlag(t *testing.T) {
	d := &Driver{cfg: mustSQSCfg()}
	attrs := d.buildQueueAttributes("orders.fifo", &queueapi.Config{})
	if assert.NotNil(t, attrs, "FIFO queue must produce attrs") {
		assert.Equal(t, "true", attrs["FifoQueue"])
	}
}

func TestBuildQueueAttributes_StandardQueueOmitsFifoFlag(t *testing.T) {
	d := &Driver{cfg: mustSQSCfg()}
	attrs := d.buildQueueAttributes("orders", &queueapi.Config{})
	if attrs != nil {
		_, present := attrs["FifoQueue"]
		assert.False(t, present, "non-FIFO queue must not declare FifoQueue")
	}
}
