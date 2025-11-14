package internode

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
)

// Keep existing structs - no changes to your data model
type encodedPayload struct {
	Format payload.Format
	Data   any
}

type encodedMessage struct {
	Topic    string
	Payloads []encodedPayload
}

type encodedPackage struct {
	Source   relay.PID
	Target   relay.PID
	Messages []*encodedMessage
}

type MessageCodec struct {
	transcoder payload.Transcoder
	handle     *codec.MsgpackHandle
	bufferPool sync.Pool
	encPkgPool sync.Pool
}

func NewMessageCodec(transcoder payload.Transcoder) *MessageCodec {
	mh := &codec.MsgpackHandle{}

	mh.MapType = reflect.TypeOf(map[string]any(nil))
	mh.SliceType = reflect.TypeOf([]any(nil))

	if err := registerPIDExtension(mh); err != nil {
		panic(fmt.Errorf("failed to register relay.PID extension: %w", err))
	}

	return &MessageCodec{
		transcoder: transcoder,
		handle:     mh,
		bufferPool: sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
		encPkgPool: sync.Pool{
			New: func() any {
				return &encodedPackage{Messages: make([]*encodedMessage, 0, 8)}
			},
		},
	}
}

func (c *MessageCodec) resetEncodedPackage(p *encodedPackage) {
	p.Source = relay.PID{}
	p.Target = relay.PID{}

	for i := range p.Messages {
		p.Messages[i] = nil
	}
	p.Messages = p.Messages[:0]
}

func (c *MessageCodec) Encode(pkg *relay.Package) ([]byte, error) {
	encPkg := c.encPkgPool.Get().(*encodedPackage)
	defer func() {
		c.resetEncodedPackage(encPkg)
		c.encPkgPool.Put(encPkg)
	}()

	encPkg.Source = pkg.Source
	encPkg.Target = pkg.Target

	if cap(encPkg.Messages) < len(pkg.Messages) {
		encPkg.Messages = make([]*encodedMessage, len(pkg.Messages))
	} else {
		encPkg.Messages = encPkg.Messages[:len(pkg.Messages)]
	}

	for i, msg := range pkg.Messages {
		encMsg := &encodedMessage{
			Topic:    msg.Topic,
			Payloads: make([]encodedPayload, len(msg.Payloads)),
		}

		for j, p := range msg.Payloads {
			normalizedPayload, err := c.normalizePayload(p)
			if err != nil {
				return nil, fmt.Errorf("failed to transcode payload at message %d, payload %d: %w", i, j, err)
			}
			encMsg.Payloads[j] = encodedPayload{
				Format: normalizedPayload.Format(),
				Data:   normalizedPayload.Data(),
			}
		}
		encPkg.Messages[i] = encMsg
	}

	buf := c.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer c.bufferPool.Put(buf)

	encoder := codec.NewEncoder(buf, c.handle)
	if err := encoder.Encode(encPkg); err != nil {
		return nil, fmt.Errorf("failed to msgpack encode package: %w", err)
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

func (c *MessageCodec) Decode(data []byte) (*relay.Package, error) {
	encPkg := c.encPkgPool.Get().(*encodedPackage)
	defer func() {
		c.resetEncodedPackage(encPkg)
		c.encPkgPool.Put(encPkg)
	}()

	decoder := codec.NewDecoder(bytes.NewReader(data), c.handle)
	if err := decoder.Decode(encPkg); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("failed to msgpack decode package: buffer is empty or incomplete")
		}
		return nil, fmt.Errorf("failed to msgpack decode package: %w", err)
	}

	finalPkg := relay.AcquirePackage()
	finalPkg.Source = encPkg.Source
	finalPkg.Target = encPkg.Target

	// Reuse existing Messages slice capacity if possible
	if cap(finalPkg.Messages) < len(encPkg.Messages) {
		finalPkg.Messages = make([]*relay.Message, len(encPkg.Messages))
	} else {
		finalPkg.Messages = finalPkg.Messages[:len(encPkg.Messages)]
	}

	for i, encMsg := range encPkg.Messages {
		finalMsg := &relay.Message{
			Topic:    encMsg.Topic,
			Payloads: make(payload.Payloads, len(encMsg.Payloads)),
		}

		for j, encP := range encMsg.Payloads {
			finalMsg.Payloads[j] = payload.NewPayload(encP.Data, encP.Format)
		}
		finalPkg.Messages[i] = finalMsg
	}

	return finalPkg, nil
}

// Keep your existing normalization logic exactly as-is
func (c *MessageCodec) normalizePayload(p payload.Payload) (payload.Payload, error) {
	switch p.Format() {
	case payload.Golang, payload.String, payload.Bytes, payload.Error:
		return p, nil
	case payload.JSON, payload.YAML, payload.Lua:
		return c.transcoder.Transcode(p, payload.JSON)
	default:
		return c.transcoder.Transcode(p, payload.JSON)
	}
}

func registerPIDExtension(mh *codec.MsgpackHandle) error {
	err := mh.AddExt(
		reflect.TypeOf(relay.PID{}),
		1, // extension tag
		// Encode function: reflect.Value -> []byte
		// Note: v2 passes structs as pointers to the encode function
		func(v reflect.Value) ([]byte, error) {
			// For struct types, v2 passes a pointer to the value
			var pid *relay.PID
			if v.Kind() == reflect.Ptr {
				pid = v.Interface().(*relay.PID)
			} else {
				// Fallback for non-pointer case
				p := v.Interface().(relay.PID)
				pid = &p
			}

			// Convert PID string representation to bytes
			return []byte(pid.String()), nil
		},
		// Decode function: (reflect.Value, []byte) -> error
		func(v reflect.Value, data []byte) error {
			// Parse PID from byte data
			pid, err := relay.ParsePID(string(data))
			if err != nil {
				return fmt.Errorf("failed to parse PID: %w", err)
			}

			// v2 passes a pointer for struct types
			if v.Kind() == reflect.Ptr {
				if !v.Elem().CanSet() {
					return fmt.Errorf("cannot set PID value: value is not settable")
				}
				v.Elem().Set(reflect.ValueOf(pid))
			} else {
				if !v.CanSet() {
					return fmt.Errorf("cannot set PID value: value is not settable")
				}
				v.Set(reflect.ValueOf(pid))
			}
			return nil
		},
	)

	return err
}
