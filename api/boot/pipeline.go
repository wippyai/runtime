// SPDX-License-Identifier: MPL-2.0

// Package boot provides application boot and component loading.
package boot

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
)

type (
	// Stage processes registry entries in a build pipeline.
	// Stages are executed sequentially in the order they are added to the pipeline.
	Stage interface {
		// Name returns the stage identifier used in error messages.
		Name() string

		// Execute processes the entries. It receives a pointer to the entries slice
		// and can modify it in place. If an error is returned, pipeline execution stops.
		Execute(ctx context.Context, entries *[]registry.Entry) error
	}

	// Pipeline executes a sequence of stages to build and transform registry entries.
	// Each stage processes the entries in order, and execution stops on first error.
	Pipeline interface {
		// Execute runs all pipeline stages in sequence.
		// If any stage returns an error, execution stops and the error is returned.
		Execute(ctx context.Context, entries *[]registry.Entry) error
	}
)
