package errors

import (
	failurepb "go.temporal.io/api/failure/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
)

const (
	// FailureSource is the error source written into Temporal failures from Wippy.
	FailureSource = "WippySDK"
)

// NewFailureConverter creates a failure converter that preserves Temporal default
// behavior while stamping Wippy source metadata.
func NewFailureConverter(dc converter.DataConverter) converter.FailureConverter {
	base := temporal.NewDefaultFailureConverter(temporal.DefaultFailureConverterOptions{
		DataConverter: dc,
	})
	return &sourceFailureConverter{
		base:   base,
		source: FailureSource,
	}
}

type sourceFailureConverter struct {
	base   converter.FailureConverter
	source string
}

func (c *sourceFailureConverter) ErrorToFailure(err error) *failurepb.Failure {
	failure := c.base.ErrorToFailure(err)
	if failure == nil {
		return nil
	}
	failure.Source = c.source
	return failure
}

func (c *sourceFailureConverter) FailureToError(failure *failurepb.Failure) error {
	return c.base.FailureToError(failure)
}
