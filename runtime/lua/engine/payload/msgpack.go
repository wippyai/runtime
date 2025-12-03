package payload

import (
	"bytes"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

var (
	msgpackHandle     *codec.MsgpackHandle
	msgpackHandleOnce sync.Once
	msgpackBufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
	msgpackReaderPool = sync.Pool{
		New: func() any {
			return new(bytes.Reader)
		},
	}
)

func getMsgpackHandle() *codec.MsgpackHandle {
	msgpackHandleOnce.Do(func() {
		msgpackHandle = &codec.MsgpackHandle{}
		msgpackHandle.MapType = reflect.TypeOf(map[string]any(nil))
		msgpackHandle.SliceType = reflect.TypeOf([]any(nil))
	})
	return msgpackHandle
}

// RegisterMsgPack registers MsgPack<->Lua transcoders
func RegisterMsgPack(transcoder payload.TranscoderRegister) {
	to := &ToMsgPack{}
	from := &MsgPackToLua{}

	transcoder.RegisterTranscoder(payload.Lua, payload.MsgPack, 1, to)
	transcoder.RegisterTranscoder(payload.MsgPack, payload.Lua, 1, from)
}

// ToMsgPack converts a Lua payload to a MsgPack payload
type ToMsgPack struct{}

// Transcode implements payload.FormatTranscoder
func (t *ToMsgPack) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Lua {
		return nil, NewInvalidFormatError("Lua=>MsgPack can only transcode from Lua format, got " + string(p.Format()))
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, NewInvalidTypeError("Lua=>MsgPack expects lua.LValue, got " + msgpackTypeName(p.Data()))
	}

	goValue := value.ToGoAny(lv)

	buf := msgpackBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer msgpackBufferPool.Put(buf)

	encoder := codec.NewEncoder(buf, getMsgpackHandle())
	if err := encoder.Encode(goValue); err != nil {
		return nil, NewTranscodeError("failed to encode to MsgPack", err)
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())

	return payload.NewPayload(result, payload.MsgPack), nil
}

// MsgPackToLua converts a MsgPack payload to a Lua payload
type MsgPackToLua struct{}

// Transcode implements payload.FormatTranscoder
func (t *MsgPackToLua) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.MsgPack {
		return nil, NewInvalidFormatError("MsgPack=>Lua can only transcode from MsgPack format, got " + string(p.Format()))
	}

	data, ok := p.Data().([]byte)
	if !ok {
		return nil, NewInvalidTypeError("MsgPack=>Lua expects []byte, got " + msgpackTypeName(p.Data()))
	}

	reader := msgpackReaderPool.Get().(*bytes.Reader)
	reader.Reset(data)
	defer msgpackReaderPool.Put(reader)

	var goValue any
	decoder := codec.NewDecoder(reader, getMsgpackHandle())
	if err := decoder.Decode(&goValue); err != nil {
		return nil, NewTranscodeError("failed to decode MsgPack", err)
	}

	lv, err := GoToLua(goValue)
	if err != nil {
		return nil, NewTranscodeError("failed to convert to Lua", err)
	}

	return payload.NewPayload(lv, payload.Lua), nil
}

func msgpackTypeName(v any) string {
	if v == nil {
		return "nil"
	}
	return reflect.TypeOf(v).String()
}
