package otel

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func newCreateExporterError(cause error) error {
	return apierror.New(apierror.Internal, "failed to create OTLP exporter").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func newCreateResourceError(cause error) error {
	return apierror.New(apierror.Internal, "failed to create resource").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func newUnsupportedProtocolError(protocol string) error {
	return apierror.New(apierror.Invalid, "unsupported protocol").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"protocol": protocol}))
}

func newCreateMetricExporterError(cause error) error {
	return apierror.New(apierror.Internal, "failed to create OTLP metric exporter").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}

func newShutdownMeterProviderError(cause error) error {
	return apierror.New(apierror.Internal, "failed to shutdown meter provider").
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"cause": cause.Error()})).
		WithCause(cause)
}
