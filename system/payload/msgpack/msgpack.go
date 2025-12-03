package msgpack

import (
	"bytes"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/payload"
)

var (
	handle     *codec.MsgpackHandle
	handleOnce sync.Once
	bufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
	readerPool = sync.Pool{
		New: func() any {
			return new(bytes.Reader)
		},
	}
)

func getHandle() *codec.MsgpackHandle {
	handleOnce.Do(func() {
		handle = &codec.MsgpackHandle{}
		handle.MapType = reflect.TypeOf(map[string]any(nil))
		handle.SliceType = reflect.TypeOf([]any(nil))
	})
	return handle
}

// Register registers the MsgPack transcoders with the transcoder registry.
func Register(transcoder payload.TranscoderRegister) {
	to := &ToMsgPack{}
	from := &FromMsgPack{}

	transcoder.RegisterTranscoder(payload.Golang, payload.MsgPack, 1, to)
	transcoder.RegisterTranscoder(payload.MsgPack, payload.Golang, 1, from)
}

// ToMsgPack converts a Golang payload to a MsgPack payload.
type ToMsgPack struct{}

// Transcode implements payload.FormatTranscoder.
func (t *ToMsgPack) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, NewInvalidFormatError("Golang=>MsgPack", payload.Golang, p.Format())
	}

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	encoder := codec.NewEncoder(buf, getHandle())
	if err := encoder.Encode(p.Data()); err != nil {
		return nil, NewMarshalError(err)
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())

	return payload.NewPayload(result, payload.MsgPack), nil
}

// FromMsgPack converts a MsgPack payload to a Golang payload.
type FromMsgPack struct{}

// Transcode implements payload.FormatTranscoder.
func (t *FromMsgPack) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.MsgPack {
		return nil, NewInvalidFormatError("MsgPack=>Golang", payload.MsgPack, p.Format())
	}

	data, ok := p.Data().([]byte)
	if !ok {
		return nil, NewInvalidDataTypeError("MsgPack=>Golang", typeName(p.Data()))
	}

	reader := readerPool.Get().(*bytes.Reader)
	reader.Reset(data)
	defer readerPool.Put(reader)

	var result any
	decoder := codec.NewDecoder(reader, getHandle())
	if err := decoder.Decode(&result); err != nil {
		return nil, NewUnmarshalError(err)
	}

	return payload.NewPayload(result, payload.Golang), nil
}

func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return reflect.TypeOf(v).String()
}
