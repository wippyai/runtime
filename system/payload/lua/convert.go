package lua

import (
	"errors"
	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

func FromPayload(dtt payload.Transcoder, p payload.Payload) (lua.LValue, error) {
	if p == nil {
		return nil, errors.New("nil payload")
	}

	luaPayload, err := dtt.Transcode(p, payload.Lua)
	if err != nil {
		return nil, err
	}

	if luaValue, ok := luaPayload.Data().(lua.LValue); ok {
		return luaValue, nil
	}

	return nil, errors.New("invalid payload data type")
}
