// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"encoding/json"
	"net/url"

	"github.com/wippyai/runtime/api/payload"
)

// marshalBody converts a payload to JSON bytes for AMQP message body.
func marshalBody(p payload.Payload) ([]byte, error) {
	if p == nil {
		return nil, nil
	}
	return json.Marshal(p)
}

// unmarshalBody converts AMQP message body bytes to a payload.
func unmarshalBody(data []byte) payload.Payload {
	if len(data) == 0 {
		return nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return payload.NewString(string(data))
	}
	return payload.New(result)
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
