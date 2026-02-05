package boot

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/msgpack"
	"github.com/wippyai/runtime/system/payload/yaml"
)

var transcoderConfiguredKey = &ctxapi.Key{Name: "boot.payload.transcoder.configured"}

type configuredTranscoder interface {
	payload.Transcoder
	payload.TranscoderRegister
}

// ConfigureTranscoder registers canonical runtime payload formats on the given
// transcoder once per AppContext.
func ConfigureTranscoder(ctx context.Context, dtt configuredTranscoder) configuredTranscoder {
	if dtt == nil {
		return nil
	}

	appCtx := ctxapi.AppFromContext(ctx)
	if appCtx != nil {
		if configured, ok := appCtx.Get(transcoderConfiguredKey).(bool); ok && configured {
			return dtt
		}
	}

	json.Register(dtt)
	msgpack.Register(dtt)
	yaml.Register(dtt)
	luapayload.Register(dtt)

	if appCtx != nil {
		appCtx.With(transcoderConfiguredKey, true)
	}

	return dtt
}
