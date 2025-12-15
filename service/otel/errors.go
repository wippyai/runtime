package otel

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func newCreateExporterError(cause error) error {
	return apierror.New(apierror.Internal, "failed to create OTLP exporter").WithCause(cause)
}

func newCreateResourceError(cause error) error {
	return apierror.New(apierror.Internal, "failed to create resource").WithCause(cause)
}

func newUnsupportedProtocolError(protocol string) error {
	return apierror.New(apierror.Invalid, "unsupported protocol: "+protocol).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"protocol": protocol}))
}

func newShutdownTracerProviderError(cause error) error {
	return apierror.New(apierror.Internal, "failed to shutdown tracer provider").WithCause(cause)
}

func newCreateMetricExporterError(cause error) error {
	return apierror.New(apierror.Internal, "failed to create OTLP metric exporter").WithCause(cause)
}

func newShutdownMeterProviderError(cause error) error {
	return apierror.New(apierror.Internal, "failed to shutdown meter provider").WithCause(cause)
}
