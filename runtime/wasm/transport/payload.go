// SPDX-License-Identifier: MPL-2.0

package transport

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
)

// PayloadTransport maps runtime payloads directly to wasm call args/results.
type PayloadTransport struct{}

// NewPayloadTransport creates payload transport.
func NewPayloadTransport() *PayloadTransport {
	return &PayloadTransport{}
}

// Prepare converts input payloads into call arguments.
func (t *PayloadTransport) Prepare(ctx context.Context, input payload.Payloads) ([]any, error) {
	return PayloadsToArgs(ctx, input)
}

// EncodeResult wraps the result as a payload.
func (t *PayloadTransport) EncodeResult(_ context.Context, result any) (payload.Payload, error) {
	return payload.New(result), nil
}
