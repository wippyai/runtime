// SPDX-License-Identifier: MPL-2.0.

//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"github.com/wippyai/runtime/service/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TestOTLP_TracesReachJaeger validates the runtime's real OTLP trace pipeline
// end to end: service/otel.InitializeProvider builds a TracerProvider whose
// OTLP/HTTP exporter ships spans to a live Jaeger collector, and Jaeger's API
// then serves them back. Run with Jaeger up (`make otel-up`).
func TestOTLP_TracesReachJaeger(t *testing.T) {
	const (
		otlpEndpoint = "localhost:4318"
		jaegerAPI    = "http://localhost:16686"
		service      = "wippy-e2e-test"
		op           = "e2e.root"
	)

	if !reachable(otlpEndpoint, 2*time.Second) {
		t.Skipf("jaeger OTLP endpoint %s not reachable - run `make otel-up`", otlpEndpoint)
	}

	tp, err := otel.InitializeProvider(context.Background(), otelapi.Config{
		Enabled:       true,
		TracesEnabled: true,
		Endpoint:      otlpEndpoint,
		Protocol:      "http/protobuf",
		Insecure:      true,
		ServiceName:   service,
		SampleRate:    1.0,
	}, zap.NewNop())
	require.NoError(t, err)

	tracer := tp.Tracer("wippy-runtime")
	_, span := tracer.Start(context.Background(), op,
		trace.WithAttributes(attribute.String("e2e", "otel-traces")))
	span.End()

	// Shutdown flushes the BatchSpanProcessor so the span reaches Jaeger.
	require.NoError(t, otel.ShutdownTracerProvider(context.Background(), tp, zap.NewNop()))

	var found bool
	for i := 0; i < 40; i++ {
		if jaegerHasOperation(jaegerAPI, service, op) {
			found = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	assert.True(t, found, "span %q for service %q never appeared in Jaeger", op, service)
}

func jaegerHasOperation(jaegerAPI, service, op string) bool {
	url := fmt.Sprintf("%s/api/traces?service=%s&limit=50", jaegerAPI, service)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	var parsed struct {
		Data []struct {
			Spans []struct {
				OperationName string `json:"operationName"`
			} `json:"spans"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return false
	}
	for _, tr := range parsed.Data {
		for _, sp := range tr.Spans {
			if sp.OperationName == op {
				return true
			}
		}
	}
	return false
}

func reachable(addr string, timeout time.Duration) bool {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
