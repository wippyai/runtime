// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"fmt"
	"net/url"

	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// queueCodec returns the wire codec format for a queue, defaulting to JSON.
func queueCodec(cfg *queueapi.Config) string {
	if cfg != nil && cfg.Codec != "" {
		return cfg.Codec
	}
	return payload.JSON
}

// marshalBody serializes a payload to wire bytes in the given codec format.
// The transcoder resolves the conversion path; callers never hop through an
// intermediate format themselves.
func marshalBody(tc payload.Transcoder, codec string, p payload.Payload) ([]byte, error) {
	if p == nil {
		return nil, nil
	}
	encoded, err := tc.Transcode(p, codec)
	if err != nil {
		return nil, fmt.Errorf("transcode to %s: %w", codec, err)
	}
	switch v := encoded.Data().(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("codec %s: expected []byte or string, got %T", codec, v)
	}
}

// sanitizeURL removes credentials from an AMQP URL for safe logging.
func sanitizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	u.User = nil
	return u.String()
}
