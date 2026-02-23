// SPDX-License-Identifier: MPL-2.0

// Package runtime provides runtime execution and command management.
package runtime

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

type (
	// Options is an alias to attrs.Attributes for task options configuration.
	Options = attrs.Attributes

	// Bag is an alias to attrs.Bag for task options storage.
	Bag = attrs.Bag

	// Task represents a unit of work to be executed by the runtime system.
	Task struct {
		// ID uniquely identifies the function/process/operation to be executed
		ID registry.ID `json:"id"`

		// Payloads contains the input data for the function execution
		Payloads payload.Payloads `json:"payloads"`

		// Options contains runtime options for this task execution (retry, timeout, etc.)
		Options Options `json:"options,omitempty"`

		// Context contains context overrides to apply when executing this task.
		// These pairs are set in the new FrameContext after inheritance but before sealing.
		// Can include actor, scope, custom values, or any other context keys.
		Context []context.Pair `json:"context,omitempty"`
	}

	// Result represents the outcome of an executed task.
	// It can contain either a successful payload or an error,
	// allowing for both successful and failed execution handling.
	Result struct {
		// Value contains the successful execution result data
		Value payload.Payload `json:"value"`

		// Error contains any error that occurred during task execution
		Error error `json:"error"`
	}
)
