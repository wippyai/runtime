// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"context"
	"net/http"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type benchWriter struct {
	h http.Header
}

func (w *benchWriter) Header() http.Header {
	return w.h
}

func (w *benchWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *benchWriter) WriteHeader(_ int) {}

func (w *benchWriter) Flush() {}

func newBenchSession(b *testing.B) *Session {
	b.Helper()

	s, err := NewSession(
		context.Background(),
		RelayCommand{HeartbeatInterval: "0s"},
		registry.NewID("app", "server"),
		newMockHost(),
		newMockNode(),
		newMockTopology(),
		&mockTranscoder{},
		&testPIDGen{},
		zap.NewNop(),
	)
	if err != nil {
		b.Fatalf("new session: %v", err)
	}
	return s
}

func BenchmarkSSEEncoderWriteEvent(b *testing.B) {
	writer := &benchWriter{h: make(http.Header)}
	enc, err := newSSEEncoder(writer)
	if err != nil {
		b.Fatalf("new encoder: %v", err)
	}

	const data = `{"delta":"hello","id":"123","index":1}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := enc.writeEvent("llm.delta", data); err != nil {
			b.Fatalf("write event: %v", err)
		}
	}
}

func BenchmarkSessionPayloadToEventDataJSONMap(b *testing.B) {
	s := newBenchSession(b)
	p := payload.NewPayload(map[string]any{
		"delta": "hello",
		"id":    "123",
		"index": 1,
	}, payload.JSON)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.payloadToEventData(p); err != nil {
			b.Fatalf("payloadToEventData: %v", err)
		}
	}
}
