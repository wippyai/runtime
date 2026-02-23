// SPDX-License-Identifier: MPL-2.0

package transport

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/api/payload"
)

func TestPayloadTransportPrepareAndEncode(t *testing.T) {
	tr := NewPayloadTransport()

	args, err := tr.Prepare(context.Background(), payload.Payloads{payload.New(7), payload.New("x")})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("Prepare() args len = %d, want 2", len(args))
	}
	if args[0] != 7 || args[1] != "x" {
		t.Fatalf("Prepare() args = %#v, want [7 \"x\"]", args)
	}

	out, err := tr.EncodeResult(context.Background(), 42)
	if err != nil {
		t.Fatalf("EncodeResult() error = %v", err)
	}
	if out == nil || out.Data() != 42 {
		t.Fatalf("EncodeResult() payload = %#v, want data=42", out)
	}
}
