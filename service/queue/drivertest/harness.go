// SPDX-License-Identifier: MPL-2.0

package drivertest

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

// defaultTimeout is used when no custom timeout is configured.
const defaultTimeout = 5 * time.Second

// queueCounter provides unique suffixes so parallel tests never collide.
var queueCounter atomic.Uint64

// Harness runs conformance tests against a queue.Driver implementation.
//
// Create one via [New], configure it with [Option] functions, and call [Harness.Run]
// to execute the full suite as subtests of the current test.
type Harness struct {
	t      *testing.T
	driver queueapi.Driver
	cfg    config
}

type config struct {
	// timeout is the maximum time to wait for a message delivery.
	timeout time.Duration
	// preservesMessageID indicates whether the driver keeps the original message ID
	// through a publish/consume round-trip. Drivers like SQS assign their own IDs.
	preservesMessageID bool
	// preservesHeaders indicates whether the driver preserves custom headers
	// through a publish/consume round-trip.
	preservesHeaders bool
	// nackRedelivers indicates whether calling Nack on a delivery causes the
	// message to be automatically redelivered.
	nackRedelivers bool
	// getQueueInfoAccurate indicates whether GetQueueInfo returns accurate
	// (non-approximate) message counts immediately after publish. Drivers like
	// SQS return eventually-consistent approximate counts.
	getQueueInfoAccurate bool
	// supportsReattach indicates whether a new consumer can reliably receive
	// messages immediately after a previous consumer on the same queue is
	// cancelled. Drivers with consumer-group semantics (SQS) may have
	// inherent startup latency that makes this unreliable.
	supportsReattach bool
	// declareLeakDriver is the driver kind name under DriverOptions whose
	// declare-only keys must never leak into published message headers.
	declareLeakDriver string
	// declareLeakOpts lists keys/values put into DriverOptions[driver] at
	// DeclareQueue time; the leak test asserts none are present on consume.
	declareLeakOpts map[string]any
}

// Option configures a [Harness].
type Option func(*config)

// WithTimeout sets the maximum duration the harness waits for message delivery
// in each test. Defaults to 5s. Use longer values for drivers with high
// latency (e.g. SQS with long-poll).
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// WithPreservesMessageID indicates whether the driver is expected to preserve
// the original message ID through a publish/consume round-trip. Set to false
// for drivers like SQS that assign their own message IDs.
// Defaults to true.
func WithPreservesMessageID(v bool) Option {
	return func(c *config) { c.preservesMessageID = v }
}

// WithPreservesHeaders indicates whether the driver is expected to preserve
// custom message headers through a publish/consume round-trip.
// Defaults to true.
func WithPreservesHeaders(v bool) Option {
	return func(c *config) { c.preservesHeaders = v }
}

// WithNackRedelivers indicates whether calling Nack causes the driver to
// automatically redeliver the message.
// Defaults to true.
func WithNackRedelivers(v bool) Option {
	return func(c *config) { c.nackRedelivers = v }
}

// WithGetQueueInfoAccurate indicates whether GetQueueInfo returns accurate
// message counts immediately after publish. Set to false for drivers like SQS
// where ApproximateNumberOfMessages is eventually consistent.
// Defaults to true.
func WithGetQueueInfoAccurate(v bool) Option {
	return func(c *config) { c.getQueueInfoAccurate = v }
}

// WithSupportsReattach indicates whether a new consumer can immediately
// receive messages after the previous consumer on the same queue is cancelled.
// Set to false for drivers with consumer-group semantics (SQS) where
// consumer startup latency makes this unreliable.
// Defaults to true.
func WithSupportsReattach(v bool) Option {
	return func(c *config) { c.supportsReattach = v }
}

// WithDeclareLeakProbe enables the declare-only-keys-must-not-leak test. The
// driver parameter is the sub-bag key name used in queue DriverOptions (e.g.
// "amqp", "sqs"). opts are keys placed into that sub-bag at DeclareQueue time;
// the test asserts none of these keys appear on a consumed message's Headers.
// Omit the option to skip the test (e.g. in-memory drivers that don't persist
// declare options separately from headers).
func WithDeclareLeakProbe(driver string, opts map[string]any) Option {
	return func(c *config) {
		c.declareLeakDriver = driver
		c.declareLeakOpts = opts
	}
}

// New creates a new conformance test [Harness] for the given driver.
// The driver must already be started and ready to accept operations.
func New(t *testing.T, driver queueapi.Driver, opts ...Option) *Harness {
	t.Helper()

	cfg := config{
		timeout:              defaultTimeout,
		preservesMessageID:   true,
		preservesHeaders:     true,
		nackRedelivers:       true,
		getQueueInfoAccurate: true,
		supportsReattach:     true,
	}
	for _, o := range opts {
		o(&cfg)
	}

	return &Harness{
		t:      t,
		driver: driver,
		cfg:    cfg,
	}
}

// uniqueID returns a queue registry.ID that is unique across the entire test run.
func (h *Harness) uniqueID(prefix string) registry.ID {
	n := queueCounter.Add(1)
	return registry.ParseID(fmt.Sprintf("test:%s-%d", prefix, n))
}

// uniqueQueueName returns a queue name that is unique across the entire test run.
func (h *Harness) uniqueQueueName(prefix string) string {
	n := queueCounter.Add(1)
	return fmt.Sprintf("%s-%d-%s", prefix, n, time.Now().Format("150405"))
}
