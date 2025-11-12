package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
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
