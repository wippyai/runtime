package internode

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
)

// encodedPayload is a simple, gob-friendly struct used to serialize a payload.
// We convert the payload.Payload interface into this DTO (Data Transfer Object) before encoding.
type encodedPayload struct {
	Format payload.Format
	Data   any
}

// encodedMessage is a gob-friendly representation of a pubsub.Message.
type encodedMessage struct {
	Topic    string
	Payloads []encodedPayload
}

// encodedPackage is a gob-friendly representation of a pubsub.Package.
type encodedPackage struct {
	Source   pubsub.PID
	Target   pubsub.PID
	Messages []*encodedMessage
}

// MessageCodec handles Package <-> bytes conversion using gob encoding
// with optimizations for pool reuse and payload normalization.
type MessageCodec struct {
	transcoder payload.Transcoder
	bufferPool sync.Pool // Pool for reusing bytes.Buffer to reduce allocations.
	encPkgPool sync.Pool // Pool for reusing the encodedPackage DTO.
}

// NewMessageCodec creates a new codec for message serialization.
func NewMessageCodec(transcoder payload.Transcoder) *MessageCodec {
	// We MUST register any concrete types that might be stored in an `any`
	// field (like the payload.Data). This is a requirement of encoding/gob.
	gob.Register(pubsub.PID{})
	gob.Register(registry.ID{})
	gob.Register(event.Event{})
	gob.Register(registry.Entry{})
	gob.Register(registry.Metadata{})

	return &MessageCodec{
		transcoder: transcoder,
		bufferPool: sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
		encPkgPool: sync.Pool{
			New: func() any {
				// Pre-allocating a small slice can be a micro-optimization.
				return &encodedPackage{Messages: make([]*encodedMessage, 0, 8)}
			},
		},
	}
}

// resetEncodedPackage clears an encodedPackage so it can be safely returned to a pool.
func (c *MessageCodec) resetEncodedPackage(p *encodedPackage) {
	// Clear Source and Target PIDs
	p.Source = pubsub.PID{}
	p.Target = pubsub.PID{}

	// Nil out pointers to allow GC and prevent data leakage between usages.
	for i := range p.Messages {
		p.Messages[i] = nil
	}
	p.Messages = p.Messages[:0]
}

// Encode converts a pubsub.Package to bytes by first transforming it into a
// gob-friendly intermediate representation. It uses pooling to minimize allocations.
func (c *MessageCodec) Encode(pkg *pubsub.Package) ([]byte, error) {
	encPkg := c.encPkgPool.Get().(*encodedPackage)
	defer func() {
		c.resetEncodedPackage(encPkg)
		c.encPkgPool.Put(encPkg)
	}()

	encPkg.Source = pkg.Source
	encPkg.Target = pkg.Target

	// Reuse the underlying slice if it has enough capacity.
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

	encoder := gob.NewEncoder(buf)
	if err := encoder.Encode(encPkg); err != nil {
		return nil, fmt.Errorf("failed to gob encode package: %w", err)
	}

	// We must return a copy of the buffer's bytes because the buffer is pooled
	// and its underlying array will be overwritten on next use.
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())

	return result, nil
}

// Decode converts bytes back to a pubsub.Package. It reuses the intermediate DTO.
func (c *MessageCodec) Decode(data []byte) (*pubsub.Package, error) {
	encPkg := c.encPkgPool.Get().(*encodedPackage)
	defer func() {
		c.resetEncodedPackage(encPkg)
		c.encPkgPool.Put(encPkg)
	}()

	decoder := gob.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(encPkg); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("failed to gob decode package: buffer is empty or incomplete")
		}
		return nil, fmt.Errorf("failed to gob decode package: %w", err)
	}

	finalPkg := &pubsub.Package{
		Source:   encPkg.Source,
		Target:   encPkg.Target,
		Messages: make([]*pubsub.Message, len(encPkg.Messages)),
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

// normalizePayload ensures a payload's data is in a gob-safe format.
// It converts complex payload formats to JSON for reliable serialization.
func (c *MessageCodec) normalizePayload(p payload.Payload) (payload.Payload, error) {
	switch p.Format() {
	case payload.Golang, payload.String, payload.Bytes, payload.Error:
		// These types are native or simple enough for gob to handle.
		return p, nil
	case payload.JSON, payload.YAML, payload.Lua:
		// For these formats, convert to our universal, simple format (a JSON string).
		return c.transcoder.Transcode(p, payload.JSON)
	default:
		// For any other unknown formats, convert to JSON as well.
		return c.transcoder.Transcode(p, payload.JSON)
	}
}
