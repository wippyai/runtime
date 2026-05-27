// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"fmt"

	"github.com/wippyai/runtime/api/payload"
)

// marshalBody serializes a payload to wire bytes in the given codec format.
// SQS requires a non-empty body, so nil payloads are rejected by the driver
// before reaching this helper.
func marshalBody(tc payload.Transcoder, codec string, p payload.Payload) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("sqs: message body is required")
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
