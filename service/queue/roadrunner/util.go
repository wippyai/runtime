// SPDX-License-Identifier: MPL-2.0

package roadrunner

import (
	"encoding/json"

	"github.com/wippyai/runtime/api/payload"
)

// marshalBody converts a payload to JSON bytes for RoadRunner job payload.
func marshalBody(p payload.Payload) ([]byte, error) {
	if p == nil {
		return nil, nil
	}
	return json.Marshal(p)
}
