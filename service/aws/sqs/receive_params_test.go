// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// TestResolveReceiveParams_Defaults asserts the receive parameter defaults
// applied when no consumer options are provided.
func TestResolveReceiveParams_Defaults(t *testing.T) {
	maxMessages, waitTime, visTimeout := resolveReceiveParams(nil)
	assert.Equal(t, int32(10), maxMessages, "default max messages")
	assert.Equal(t, int32(20), waitTime, "default long-poll wait")
	assert.Equal(t, int32(0), visTimeout, "default visibility timeout")
}

// TestResolveReceiveParams_ConsumerDriverBag asserts that wait_time and
// visibility_timeout set on the consumer's driver bag are honored.
func TestResolveReceiveParams_ConsumerDriverBag(t *testing.T) {
	drvBag := attrs.NewBag()
	drvBag.Set("wait_time", 5)
	drvBag.Set("visibility_timeout", 60)
	rootBag := attrs.NewBag()
	rootBag.Set("sqs", drvBag)

	opts := &queueapi.ConsumerOptions{DriverOptions: rootBag}
	maxMessages, waitTime, visTimeout := resolveReceiveParams(opts)

	assert.Equal(t, int32(10), maxMessages)
	assert.Equal(t, int32(5), waitTime)
	assert.Equal(t, int32(60), visTimeout)
}

// TestResolveReceiveParams_PrefetchCap asserts that Prefetch maps to
// MaxNumberOfMessages but is clamped to SQS's hard batch limit of 10.
func TestResolveReceiveParams_PrefetchCap(t *testing.T) {
	opts := &queueapi.ConsumerOptions{Prefetch: 25}
	maxMessages, _, _ := resolveReceiveParams(opts)
	assert.Equal(t, int32(10), maxMessages, "prefetch must be clamped to 10")
}

// TestResolveReceiveParams_PrefetchBelowCap honors smaller prefetch values.
func TestResolveReceiveParams_PrefetchBelowCap(t *testing.T) {
	opts := &queueapi.ConsumerOptions{Prefetch: 3}
	maxMessages, _, _ := resolveReceiveParams(opts)
	assert.Equal(t, int32(3), maxMessages)
}
