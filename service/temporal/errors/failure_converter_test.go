package errors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"go.temporal.io/sdk/converter"
)

func TestNewFailureConverter_SetsWippySource(t *testing.T) {
	fc := NewFailureConverter(converter.GetDefaultDataConverter())
	require.NotNil(t, fc)

	err := apierror.New(apierror.NotFound, "missing")
	f := fc.ErrorToFailure(err)
	require.NotNil(t, f)
	assert.Equal(t, FailureSource, f.Source)
}

func TestNewFailureConverter_RoundTrip(t *testing.T) {
	fc := NewFailureConverter(converter.GetDefaultDataConverter())

	err := apierror.New(apierror.Invalid, "bad input")
	f := fc.ErrorToFailure(err)
	require.NotNil(t, f)

	decoded := fc.FailureToError(f)
	require.Error(t, decoded)
	assert.Contains(t, decoded.Error(), "bad input")
}
