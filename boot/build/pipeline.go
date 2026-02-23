// SPDX-License-Identifier: MPL-2.0

package build

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/registry"
)

// pipeline implements boot.Pipeline
type pipeline struct {
	stages []boot.Stage
}

// New creates a new pipeline with the given stages.
// Stages are executed in the order they are provided.
func New(stages ...boot.Stage) boot.Pipeline {
	return &pipeline{
		stages: stages,
	}
}

// Execute runs all pipeline stages in sequence.
// If a stage returns an error, execution stops and the error is wrapped
// with the stage name for better debugging.
func (p *pipeline) Execute(ctx context.Context, entries *[]registry.Entry) error {
	for _, s := range p.stages {
		if err := s.Execute(ctx, entries); err != nil {
			return NewStageError(s.Name(), err)
		}
	}
	return nil
}
