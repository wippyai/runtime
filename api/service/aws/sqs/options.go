// SPDX-License-Identifier: MPL-2.0

package sqs

// SQS-specific queue option constants.
// These are per-queue options read by the SQS driver during Attach()
// and DeclareQueue().
const (
	// OptionMaxMessages sets the maximum number of messages to receive
	// per ReceiveMessage API call. Valid range: 1-10. Default: 10.
	OptionMaxMessages = "max_messages"

	// OptionWaitTime sets the long-poll wait time in seconds.
	// Valid range: 0-20. Default: 20.
	OptionWaitTime = "wait_time"

	// OptionVisibilityTimeout sets the visibility timeout in seconds.
	// Valid range: 0-43200. Default: 0 (SQS default).
	// Used both for ReceiveMessage and CreateQueue attributes.
	OptionVisibilityTimeout = "visibility_timeout"
)
