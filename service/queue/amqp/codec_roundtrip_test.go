// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonxc "github.com/wippyai/runtime/system/payload/json"
	msgpackxc "github.com/wippyai/runtime/system/payload/msgpack"
)

// newCodecTranscoder wires the real json+msgpack transcoders into a fresh
// registry so tests exercise the exact code path the driver uses in
// production, not a mocked fake.
func newCodecTranscoder() payload.Transcoder {
	tc := syspayload.NewTranscoder()
	jsonxc.Register(tc)
	msgpackxc.Register(tc)
	return tc
}

// Payloads enter the driver as Golang-format values and must leave as wire
// bytes in the queue's configured codec. JSON is the default when Config.Codec
// is empty; marshalBody should route the Golang->JSON transcoder path.
func TestMarshalBody_JSONRoundTrip(t *testing.T) {
	tc := newCodecTranscoder()

	src := map[string]any{
		"action": "send_email",
		"user":   "alice",
		"count":  float64(3),
	}
	p := payload.NewPayload(src, payload.Golang)

	body, err := marshalBody(tc, queueCodec(&queueapi.Config{Codec: payload.JSON}), p)
	require.NoError(t, err)
	require.NotEmpty(t, body)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, src, decoded)
}

// MsgPack is the non-default codec. A queue declared with codec: msgpack must
// publish msgpack-framed bytes; those bytes must round-trip back through the
// MsgPack->Golang transcoder carrying the original key/value content. The
// msgpack library in use returns string values as []byte on decode (it does
// not set RawToString on its handle), so assertions compare content rather
// than Go type identity.
func TestMarshalBody_MsgPackRoundTrip(t *testing.T) {
	tc := newCodecTranscoder()

	src := map[string]any{
		"action": "send_email",
		"user":   "alice",
		"count":  int64(3),
	}
	p := payload.NewPayload(src, payload.Golang)

	body, err := marshalBody(tc, queueCodec(&queueapi.Config{Codec: payload.MsgPack}), p)
	require.NoError(t, err)
	require.NotEmpty(t, body)

	// MsgPack is not JSON-parseable; prove we did not accidentally fall back.
	var asJSON any
	require.Error(t, json.Unmarshal(body, &asJSON),
		"msgpack bytes must not parse as JSON — fallback would defeat the codec")

	wire := payload.NewPayload(body, payload.MsgPack)
	decoded, err := tc.Transcode(wire, payload.Golang)
	require.NoError(t, err)

	m, ok := decoded.Data().(map[string]any)
	require.True(t, ok, "msgpack decode must yield a string-keyed map")
	require.Equal(t, "send_email", asString(t, m["action"]))
	require.Equal(t, "alice", asString(t, m["user"]))
	require.Equal(t, int64(3), asInt64(t, m["count"]))
}

// asString normalises msgpack's []byte-for-strings convention so tests assert
// on content rather than representation.
func asString(t *testing.T, v any) string {
	t.Helper()
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		t.Fatalf("expected string or []byte, got %T: %v", v, v)
		return ""
	}
}

func asInt64(t *testing.T, v any) int64 {
	t.Helper()
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case uint64:
		return int64(x)
	default:
		t.Fatalf("expected integer, got %T: %v", v, v)
		return 0
	}
}

// A queue with no explicit codec falls back to JSON per queueCodec(); regress
// here so silently dropping the default does not go unnoticed.
func TestMarshalBody_EmptyCodecDefaultsToJSON(t *testing.T) {
	tc := newCodecTranscoder()

	src := map[string]any{"k": "v"}
	p := payload.NewPayload(src, payload.Golang)

	body, err := marshalBody(tc, queueCodec(&queueapi.Config{}), p)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded),
		"empty codec must fall back to JSON")
	require.Equal(t, src, decoded)
}

// Content-type header must match what the body actually is, otherwise a
// consumer cannot pick the right decoder from the wire.
func TestCodecContentType_MatchesCodec(t *testing.T) {
	require.Equal(t, "application/json", codecContentType(payload.JSON))
	require.Equal(t, "application/json", codecContentType(""))
	require.Equal(t, "application/msgpack", codecContentType(payload.MsgPack))
}
