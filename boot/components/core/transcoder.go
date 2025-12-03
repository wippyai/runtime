package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/msgpack"
	"github.com/wippyai/runtime/system/payload/yaml"
)

func Transcoder() boot.Component {
	return boot.New(boot.P{
		Name: TranscoderName,
		Load: func(ctx context.Context) (context.Context, error) {
			dtt := transcoder.GlobalTranscoder()
			json.Register(dtt)
			yaml.Register(dtt)
			msgpack.Register(dtt)
			luapayload.Register(dtt)
			luapayload.RegisterMsgPack(dtt)
			return payload.WithTranscoder(ctx, dtt), nil
		},
	})
}
