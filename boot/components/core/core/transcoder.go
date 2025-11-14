package core

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/lua"
	"github.com/wippyai/runtime/system/payload/yaml"
)

func Transcoder() boot.Component {
	return boot.New(boot.P{
		Name:  TranscoderName,
		Phase: boot.PreInit,
		Load: func(ctx context.Context) (context.Context, error) {
			dtt := transcoder.GlobalTranscoder()
			json.Register(dtt)
			yaml.Register(dtt)
			lua.Register(dtt)
			return payload.WithTranscoder(ctx, dtt), nil
		},
	})
}
