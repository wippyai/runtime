// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"fmt"
	"net/url"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// queueCodec returns the codec format for a queue, defaulting to JSON.
func queueCodec(opts attrs.Attributes) string {
	if opts != nil {
		if codec := opts.GetString(queueapi.OptionCodec, ""); codec != "" {
			return codec
		}
	}
	return payload.JSON
}

// marshalBody converts a payload to bytes using the transcoder with the given codec format.
func marshalBody(tc payload.Transcoder, codec string, p payload.Payload) ([]byte, error) {
	if p == nil {
		return nil, nil
	}

	// Ensure we start from a golang/any payload
	src := p
	if src.Format() != payload.Golang {
		var err error
		src, err = tc.Transcode(src, payload.Golang)
		if err != nil {
			return nil, fmt.Errorf("transcode to golang: %w", err)
		}
	}

	// Transcode to the target codec format
	encoded, err := tc.Transcode(src, codec)
	if err != nil {
		return nil, fmt.Errorf("transcode to %s: %w", codec, err)
	}

	switch v := encoded.Data().(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("transcoded payload data is not []byte or string, got %T", v)
	}
}

// unmarshalBody converts message body bytes to a payload using the transcoder with the given codec format.
func unmarshalBody(tc payload.Transcoder, codec string, data []byte) payload.Payload {
	if len(data) == 0 {
		return nil
	}

	// Create a payload in the source codec format
	src := payload.NewPayload(data, codec)

	// Transcode to golang/any
	result, err := tc.Transcode(src, payload.Golang)
	if err != nil {
		// Fall back to raw string if transcoding fails
		return payload.NewString(string(data))
	}

	return payload.New(result.Data())
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
