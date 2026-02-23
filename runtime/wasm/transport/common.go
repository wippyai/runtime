// SPDX-License-Identifier: MPL-2.0

package transport

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
)

// PayloadsToArgs converts payload.Payloads to raw Go arguments,
// transcoding non-Golang formats via the context transcoder.
func PayloadsToArgs(ctx context.Context, input payload.Payloads) ([]any, error) {
	args := make([]any, 0, len(input))
	dtt := payload.GetTranscoder(ctx)

	for _, pl := range input {
		if pl == nil {
			continue
		}
		if pl.Format() != payload.Golang {
			if dtt == nil {
				return nil, runtimewasm.ErrTranscoderNotFound
			}
			transcoded, err := dtt.Transcode(pl, payload.Golang)
			if err != nil {
				return nil, runtimewasm.NewTranscodePayloadError(err)
			}
			pl = transcoded
		}
		args = append(args, pl.Data())
	}

	return args, nil
}
