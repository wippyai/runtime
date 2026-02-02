package otel

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
)

func TestNewCreateExporterError(t *testing.T) {
	cause := errors.New("connection refused")
	err := newCreateExporterError(cause)
	require.NotNil(t, err)

	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, apierror.Internal, apiErr.Kind())
	assert.Contains(t, err.Error(), "failed to create OTLP exporter")
	assert.True(t, errors.Is(err, cause))
	assert.Equal(t, "connection refused", apiErr.Details().GetString("cause", ""))
}

func TestNewCreateResourceError(t *testing.T) {
	cause := errors.New("invalid resource")
	err := newCreateResourceError(cause)
	require.NotNil(t, err)

	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, apierror.Internal, apiErr.Kind())
	assert.Contains(t, err.Error(), "failed to create resource")
	assert.True(t, errors.Is(err, cause))
	assert.Equal(t, "invalid resource", apiErr.Details().GetString("cause", ""))
}

func TestNewUnsupportedProtocolError(t *testing.T) {
	err := newUnsupportedProtocolError("ws")
	require.NotNil(t, err)

	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, apierror.Invalid, apiErr.Kind())
	assert.Contains(t, err.Error(), "unsupported protocol")

	details := apiErr.Details()
	require.NotNil(t, details)
	assert.Equal(t, "ws", details.GetString("protocol", ""))
}

func TestNewCreateMetricExporterError(t *testing.T) {
	cause := errors.New("metric export failed")
	err := newCreateMetricExporterError(cause)
	require.NotNil(t, err)

	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, apierror.Internal, apiErr.Kind())
	assert.Contains(t, err.Error(), "failed to create OTLP metric exporter")
	assert.True(t, errors.Is(err, cause))
	assert.Equal(t, "metric export failed", apiErr.Details().GetString("cause", ""))
}

func TestNewShutdownMeterProviderError(t *testing.T) {
	cause := errors.New("shutdown timeout")
	err := newShutdownMeterProviderError(cause)
	require.NotNil(t, err)

	var apiErr apierror.Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, apierror.Internal, apiErr.Kind())
	assert.Contains(t, err.Error(), "failed to shutdown meter provider")
	assert.True(t, errors.Is(err, cause))
	assert.Equal(t, "shutdown timeout", apiErr.Details().GetString("cause", ""))
}
