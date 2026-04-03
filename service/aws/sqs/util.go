// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"encoding/json"

	"github.com/wippyai/runtime/api/payload"
)

// marshalBody converts a payload to JSON bytes for SQS message body.
func marshalBody(p payload.Payload) ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}
	return json.Marshal(p)
}

// unmarshalBody converts SQS message body bytes to a payload.
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
