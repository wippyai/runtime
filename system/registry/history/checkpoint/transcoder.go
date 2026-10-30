// SPDX-License-Identifier: MPL-2.0

package checkpoint

import (
	"github.com/wippyai/runtime/api/payload"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	msgpackpayload "github.com/wippyai/runtime/system/payload/msgpack"
	yamlpayload "github.com/wippyai/runtime/system/payload/yaml"
)

// NewReplayTranscoder supplies the persisted payload formats that history can
// decode without depending on application boot configuration.
func NewReplayTranscoder() payload.Transcoder {
	transcoder := syspayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	msgpackpayload.Register(transcoder)
	yamlpayload.Register(transcoder)
	return transcoder
}
