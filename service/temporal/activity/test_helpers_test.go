package activity_test

import (
	"github.com/wippyai/runtime/api/payload"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
)

func newTestTranscoder() payload.Transcoder {
	transcoder := syspayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	msgpayload.Register(transcoder)
	return transcoder
}
