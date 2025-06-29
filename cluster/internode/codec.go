package internode

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/codec"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
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
	Source   pubsub.PID
	Target   pubsub.PID
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

	// Configure for your data types
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	mh.SliceType = reflect.TypeOf([]interface{}(nil))

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
	p.Source = pubsub.PID{}
	p.Target = pubsub.PID{}

	for i := range p.Messages {
		p.Messages[i] = nil
	}
	p.Messages = p.Messages[:0]
}

func (c *MessageCodec) Encode(pkg *pubsub.Package) ([]byte, error) {
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

func (c *MessageCodec) Decode(data []byte) (*pubsub.Package, error) {
	encPkg := c.encPkgPool.Get().(*encodedPackage)
	defer func() {
		c.resetEncodedPackage(encPkg)
		c.encPkgPool.Put(encPkg)
	}()

	decoder := codec.NewDecoder(bytes.NewReader(data), c.handle)
	if err := decoder.Decode(encPkg); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("failed to msgpack decode package: buffer is empty or incomplete")
		}
		return nil, fmt.Errorf("failed to msgpack decode package: %w", err)
	}

	finalPkg := pubsub.AcquirePackage()
	finalPkg.Source = encPkg.Source
	finalPkg.Target = encPkg.Target

	// Reuse existing Messages slice capacity if possible
	if cap(finalPkg.Messages) < len(encPkg.Messages) {
		finalPkg.Messages = make([]*pubsub.Message, len(encPkg.Messages))
	} else {
		finalPkg.Messages = finalPkg.Messages[:len(encPkg.Messages)]
	}

	for i, encMsg := range encPkg.Messages {
		finalMsg := &pubsub.Message{
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
