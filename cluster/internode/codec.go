// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type encodedPayload struct {
	Data   any
	Format payload.Format
}

type encodedMessage struct {
	Topic    string
	Payloads []encodedPayload
}

type encodedPackage struct {
	Source   pid.PID
	Target   pid.PID
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

	// WriteExt selects the MsgPack 2.0 spec: strings encode as str type and
	// []byte as bin type, so string values inside map[string]any decode back
	// as strings rather than []uint8 (the 1.0 "raw" type is ambiguous between
	// the two). The wire format is fixed across the cluster — every node must
	// run the same codec, so cluster upgrades replace all nodes together
	// rather than rolling node-by-node.
	mh.WriteExt = true

	mh.MapType = reflect.TypeOf(map[string]any(nil))
	mh.SliceType = reflect.TypeOf([]any(nil))

	if err := registerPIDExtension(mh); err != nil {
		// Logical invariant: PID extension registration should always succeed.
		panic(NewRegisterPIDExtensionError(err))
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
			Topic:    msg.Topic,
			Payloads: make([]encodedPayload, len(msg.Payloads)),
		}

		for j, p := range msg.Payloads {
			normalizedPayload, err := c.normalizePayload(p)
			if err != nil {
				return nil, NewEncodePayloadError(j, err)
			}
			encMsg.Payloads[j] = encodedPayload{
				Format: normalizedPayload.Format(),
				Data:   encodeData(normalizedPayload),
			}
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
		finalMsg.Payloads = make(payload.Payloads, len(encMsg.Payloads))

		for j, encP := range encMsg.Payloads {
			finalMsg.Payloads[j] = payload.NewPayload(encP.Data, encP.Format)
		}
		finalPkg.Messages[i] = finalMsg
	}

	return finalPkg, nil
}

// normalizePayload converts payloads to formats that msgpack can encode directly.
// Pass-through: JSON (bytes), Bytes, String, Error, Golang, MsgPack
// Transcode to Golang: Lua, YAML, and other formats
func (c *MessageCodec) normalizePayload(p payload.Payload) (payload.Payload, error) {
	switch p.Format() {
	case payload.JSON, payload.Bytes, payload.String, payload.GoError, payload.Golang, payload.MsgPack:
		return p, nil
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

type pidExtension struct{}

func (pidExtension) WriteExt(v any) []byte {
	p, ok := v.(*pid.PID)
	if !ok {
		pv, ok := v.(pid.PID)
		if ok {
			p = &pv
		} else {
			return nil
		}
	}
	return []byte(p.String())
}

func (pidExtension) ReadExt(dst any, src []byte) {
	// The msgpack ext API has no error return. A PID field that fails to
	// parse (corrupt or version-mismatched wire bytes) leaves dst at the
	// zero PID; downstream routing rejects an empty Source/Target rather
	// than acting on a wrong address, so the failure surfaces as a dropped
	// message, not silent misdelivery.
	p, err := pid.ParsePID(string(src))
	if err != nil {
		return
	}
	if pidPtr, ok := dst.(*pid.PID); ok {
		*pidPtr = p
	}
}

func registerPIDExtension(mh *codec.MsgpackHandle) error {
	return mh.SetBytesExt(
		reflect.TypeOf(pid.PID{}),
		1,
		pidExtension{},
	)
}
