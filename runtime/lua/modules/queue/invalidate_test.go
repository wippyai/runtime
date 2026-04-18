// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"testing"

	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// The consumer releases the pooled *Message back to sync.Pool as soon
// as the handler returns. Any Lua userdata that outlived the handler
// (retained in a closure, stashed in a coroutine, escaped via a global)
// must not touch the recycled Message. Delivery.Invalidate() is the
// one-shot flag the consumer flips right before ReleaseMessage — all
// Lua accessors gate on it and return a structured error instead of
// dereferencing stale fields.
func TestLuaMessage_AccessorsErrorAfterInvalidate(t *testing.T) {
	cases := []struct {
		name   string
		invoke string
	}{
		{"id", `local v, err = captured:id()`},
		{"header", `local v, err = captured:header("x")`},
		{"headers", `local v, err = captured:headers()`},
		{"ack", `local v, err = captured:ack()`},
		{"nack", `local v, err = captured:nack()`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := queueapi.NewMessageWithID("invalidate-test", payload.NewPayload("data", payload.String))
			msg.Headers.Set("x", "y")

			var acks, nacks int
			l := setupStateWithDeliveryCounters(msg, &acks, &nacks)
			defer l.Close()

			// First, capture the wrapper into a Lua global while the
			// delivery is still valid.
			if err := l.DoString(`captured = queue.message()`); err != nil {
				t.Fatalf("capture: %v", err)
			}

			// Consumer-side: flip the released flag before any further
			// accessor call, exactly like processDelivery's defer runs
			// Invalidate() immediately before ReleaseMessage.
			delivery, ok := queueapi.GetDelivery(l.Context())
			if !ok {
				t.Fatal("delivery not in ctx")
			}
			delivery.Invalidate()

			// Now invoke the accessor and expect a structured INVALID
			// error instead of the stale field read.
			script := tc.invoke + `
				if v ~= nil then error("expected nil result, got: " .. tostring(v)) end
				if not err then error("expected error") end
				if err:kind() ~= errors.INVALID then error("expected INVALID, got: " .. tostring(err:kind())) end
			`
			if err := l.DoString(script); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			if acks != 0 || nacks != 0 {
				t.Errorf("%s: expected no ack/nack side effects after invalidate, got acks=%d nacks=%d",
					tc.name, acks, nacks)
			}
		})
	}
}
