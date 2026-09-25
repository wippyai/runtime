// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type encodedPayload struct {
	_struct struct{} `codec:",toarray"` //nolint:unused // msgpack struct options
	Data    any
	Format  payload.Format
}

type encodedMessage struct {
	Topic        string
	Payloads     []encodedPayload
	PayloadBytes int64
	MaxBytes     int64
	MaxItems     int
}

type encodedPackage struct {
	Source   pid.PID
	Target   pid.PID
	Messages []*encodedMessage
}

type MessageCodec struct {
	transcoder payload.Transcoder
	handle     *codec.MsgpackHandle
	refs       map[reflect.Type]extRef
	bufferPool sync.Pool
	encPkgPool sync.Pool
}

func NewMessageCodec(transcoder payload.Transcoder) *MessageCodec {
	mh := &codec.MsgpackHandle{}

	// WriteExt selects the MsgPack 2.0 spec: strings encode as str type and
	// []byte as bin type, so string values inside map[string]any decode back
	// as strings rather than []uint8 (the 1.0 "raw" type is ambiguous between
	// the two). The wire format is fixed across the cluster — every node must
	// run the same codec, so cluster upgrades replace all nodes together
	// rather than rolling node-by-node.
	mh.WriteExt = true

	mh.MapType = reflect.TypeOf(map[string]any(nil))
	mh.SliceType = reflect.TypeOf([]any(nil))

	c := &MessageCodec{
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
	// Logical invariant: the extension table registers named struct types
	// under distinct tags, which the handle always accepts.
	if err := c.registerExtensions(); err != nil {
		panic(err)
	}
	return c
}

func (c *MessageCodec) resetEncodedPackage(p *encodedPackage) {
	p.Source = pid.PID{}
	p.Target = pid.PID{}

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
			Topic:        msg.Topic,
			Payloads:     make([]encodedPayload, len(msg.Payloads)),
			PayloadBytes: msg.PayloadBytes,
			MaxBytes:     msg.MaxBytes,
			MaxItems:     msg.MaxItems,
		}

		for j, p := range msg.Payloads {
			encoded, err := c.encodePayload(p)
			if err != nil {
				return nil, NewEncodePayloadError(j, err)
			}
			encMsg.Payloads[j] = encoded
		}
		encPkg.Messages[i] = encMsg
	}

	buf := c.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer c.bufferPool.Put(buf)

	encoder := codec.NewEncoder(buf, c.handle)
	if err := encoder.Encode(encPkg); err != nil {
		return nil, NewMsgpackEncodeError(err)
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
		isEmptyOrIncomplete := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
		return nil, NewMsgpackDecodeError(err, isEmptyOrIncomplete)
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
		finalMsg := relay.AcquireMessage()
		finalMsg.Topic = encMsg.Topic
		finalMsg.PayloadBytes = encMsg.PayloadBytes
		finalMsg.MaxBytes = encMsg.MaxBytes
		finalMsg.MaxItems = encMsg.MaxItems
		finalMsg.Payloads = make(payload.Payloads, len(encMsg.Payloads))

		finalPkg.Messages[i] = finalMsg
		for j, encP := range encMsg.Payloads {
			decoded, err := c.decodePayload(encP)
			if err != nil {
				// Slots past i still hold pooled pointers from an earlier use.
				finalPkg.Messages = finalPkg.Messages[:i+1]
				relay.ReleasePackage(finalPkg)
				return nil, newDecodePayloadError(j, err)
			}
			finalMsg.Payloads[j] = decoded
		}
	}

	return finalPkg, nil
}

// encodePayload converts a payload to its wire form.
func (c *MessageCodec) encodePayload(p payload.Payload) (encodedPayload, error) {
	normalized, err := c.normalizePayload(p)
	if err != nil {
		return encodedPayload{}, err
	}
	return encodedPayload{Format: normalized.Format(), Data: encodeData(normalized)}, nil
}

// decodePayload restores a payload from its wire form. Error payloads return
// to Go errors; topology events return to the pointers their producers send.
func (c *MessageCodec) decodePayload(w encodedPayload) (payload.Payload, error) {
	switch w.Format {
	case payload.GoError:
		chain, ok := w.Data.(apierror.Chain)
		if !ok {
			return nil, newWireTypeError(w.Format, w.Data)
		}
		return payload.NewError(apierror.FromChain(&chain)), nil
	case payload.Golang:
		if w.Data != nil {
			if ref, ok := c.refs[reflect.TypeOf(w.Data)]; ok {
				return payload.NewPayload(ref.ref(w.Data), w.Format), nil
			}
		}
	}
	return payload.NewPayload(w.Data, w.Format), nil
}

// normalizePayload converts payloads to formats that msgpack can encode directly.
// Pass-through: JSON (bytes), Bytes, String, Golang, MsgPack
// Error: converted to its apierror.Chain
// Transcode to Golang: Lua, YAML, and other formats
func (c *MessageCodec) normalizePayload(p payload.Payload) (payload.Payload, error) {
	switch p.Format() {
	case payload.JSON, payload.Bytes, payload.String, payload.Golang, payload.MsgPack:
		return p, nil
	case payload.GoError:
		err, ok := p.Data().(error)
		if !ok {
			return nil, newWireTypeError(payload.GoError, p.Data())
		}
		return payload.NewPayload(apierror.BuildChain(err), payload.GoError), nil
	default:
		return c.transcoder.Transcode(payload.Snapshot(p), payload.Golang)
	}
}

func encodeData(p payload.Payload) any {
	if p.Format() == payload.Golang {
		return payload.SnapshotData(p.Data())
	}
	return p.Data()
}
