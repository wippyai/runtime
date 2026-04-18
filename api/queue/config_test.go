// SPDX-License-Identifier: MPL-2.0

package queue_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// User-facing codec aliases in YAML ("json", "msgpack") must resolve to the
// registered payload.Format strings so the transcoder can find a conversion
// path at publish/receive time.
func TestCanonicalCodec(t *testing.T) {
	cases := map[string]string{
		"":        payload.JSON,
		"json":    payload.JSON,
		"msgpack": payload.MsgPack,
		"yaml":    payload.YAML,
		"bytes":   payload.Bytes,
		"string":  payload.String,
	}
	for in, want := range cases {
		require.Equal(t, want, queueapi.CanonicalCodec(in), "alias %q", in)
	}

	// Unknown values pass through unchanged so drivers can still accept
	// codecs that aren't part of the canonical alias table.
	require.Equal(t, "custom/format", queueapi.CanonicalCodec("custom/format"))
}
