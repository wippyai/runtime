// SPDX-License-Identifier: MPL-2.0

package amqp

// AMQP-specific queue option constants.
// These are per-queue options read by the AMQP driver during Attach().
const (
	// OptionExclusive requests an exclusive consumer.
	// When true, only this consumer can access the queue.
	OptionExclusive = "exclusive"

	// OptionNoLocal prevents delivery of messages published on the same connection.
	OptionNoLocal = "no_local"

	// OptionNoWait instructs the server not to respond to the consume request.
	OptionNoWait = "no_wait"

	// OptionConsumerTag sets a custom consumer tag prefix.
	// A unique suffix is always appended automatically.
	OptionConsumerTag = "consumer_tag"
)
