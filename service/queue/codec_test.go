// SPDX-License-Identifier: MPL-2.0

package queue_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	jsonxc "github.com/wippyai/runtime/runtime/lua/engine/payload"
	queuesvc "github.com/wippyai/runtime/service/queue"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonreg "github.com/wippyai/runtime/system/payload/json"
)

func newCodecTranscoder() payload.Transcoder {
	tc := syspayload.NewTranscoder()
	jsonreg.Register(tc)
	jsonxc.Register(tc)
	return tc
}

func TestValidateCodec_RegisteredFormatOK(t *testing.T) {
	tc := newCodecTranscoder()
	require.NoError(t, queuesvc.ValidateCodec(tc, payload.JSON))
}

func TestValidateCodec_EmptyFallsBackToJSON(t *testing.T) {
	tc := newCodecTranscoder()
	require.NoError(t, queuesvc.ValidateCodec(tc, ""))
}

// A codec value that doesn't match any registered format must surface as a
// structured Invalid error up-front rather than silently failing at first
// Publish. Drivers call ValidateCodec from DeclareQueue so misconfiguration
// shows up when the registry entry loads.
func TestValidateCodec_UnknownCodecRejected(t *testing.T) {
	tc := newCodecTranscoder()
	err := queuesvc.ValidateCodec(tc, "not-a-real-format")
	require.Error(t, err)

	var rich apierror.Error
	require.True(t, errors.As(err, &rich), "error must be structured apierror")
	require.Equal(t, apierror.Invalid, rich.Kind())
}

func TestValidateCodec_NilTranscoderPassthrough(t *testing.T) {
	require.NoError(t, queuesvc.ValidateCodec(nil, payload.JSON))
}
