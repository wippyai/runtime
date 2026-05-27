// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
)

// ValidateCodec probes the transcoder for a path from the runtime's native
// Golang format to the named codec. Drivers call this at DeclareQueue time
// so a queue with an unregistered codec fails up-front rather than on the
// first Publish.
func ValidateCodec(tc payload.Transcoder, codec string) error {
	if tc == nil {
		return nil
	}
	if codec == "" {
		codec = payload.JSON
	}
	probe := payload.NewPayload(map[string]any{}, payload.Golang)
	if _, err := tc.Transcode(probe, codec); err != nil {
		return apierror.New(apierror.Invalid, "unknown payload codec").
			WithRetryable(apierror.False).
			WithDetails(attrs.NewBagFrom(map[string]any{"codec": codec, "cause": err.Error()})).
			WithCause(err)
	}
	return nil
}
