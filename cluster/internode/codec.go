// file: internode/codec.go
package internode

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
)

// MessageCodec handles Package <-> bytes conversion. It uses a Transcoder
// to normalize complex data formats into a serializable format before encoding.
type MessageCodec struct {
	transcoder payload.Transcoder
}

// NewMessageCodec creates a new codec that will use the provided transcoder for
// payload normalization. It also registers all necessary types with the gob package.
func NewMessageCodec(transcoder payload.Transcoder) *MessageCodec {
	// Register the concrete carrier type for the payload.Payload interface.
	// This is ALWAYS required when using interfaces with gob.
	gob.Register(payload.DefaultCarrier{})

	// Register other types that are part of the pubsub.Package structure or can be sent.
	gob.Register(&pubsub.Package{})
	gob.Register(&pubsub.Message{})
	gob.Register(pubsub.PID{})
	gob.Register(registry.ID{})
	gob.Register(event.Event{})
	gob.Register(registry.Entry{})
	gob.Register(registry.Metadata{})

	return &MessageCodec{
		transcoder: transcoder,
	}
}

// Encode converts a pubsub.Package to bytes. It iterates through all payloads
// and uses the transcoder to convert any non-native formats (like Lua, YAML, etc.)
// into JSON strings before passing the package to the gob encoder.
func (c *MessageCodec) Encode(pkg *pubsub.Package) ([]byte, error) {
	// Create a deep copy of the package to avoid modifying the original data,
	// which would be an unexpected side effect for the caller.
	pkgForEncoding := *pkg
	pkgForEncoding.Messages = make([]*pubsub.Message, len(pkg.Messages))

	for i, msg := range pkg.Messages {
		newMsg := *msg
		newMsg.Payloads = make([]payload.Payload, len(msg.Payloads))

		for j, p := range msg.Payloads {
			var normalizedPayload payload.Payload
			var err error

			// This is the normalization logic. We only let specific, gob-safe
			// formats pass through. Everything else is delegated to the transcoder.
			switch p.Format() {
			case payload.Golang, payload.String, payload.Bytes, payload.Error:
				// These types are native or simple and safe for gob.
				normalizedPayload = p
			default:
				// For anything else (Lua, YAML, etc.), ask the transcoder to
				// convert it to our universal format, JSON.
				normalizedPayload, err = c.transcoder.Transcode(p, payload.JSON)
				if err != nil {
					return nil, fmt.Errorf("failed to transcode payload at message %d, payload %d: %w", i, j, err)
				}
			}
			newMsg.Payloads[j] = normalizedPayload
		}
		pkgForEncoding.Messages[i] = &newMsg
	}

	// Now, encode the fully normalized package.
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(&pkgForEncoding); err != nil {
		return nil, fmt.Errorf("failed to gob encode package: %w", err)
	}

	return buf.Bytes(), nil
}

// Decode converts bytes back to a pubsub.Package using gob.
// The consumer of the decoded package is responsible for interpreting
// any normalized payloads (e.g., by unmarshaling the JSON strings).
func (c *MessageCodec) Decode(data []byte) (*pubsub.Package, error) {
	var pkg pubsub.Package
	decoder := gob.NewDecoder(bytes.NewReader(data))

	if err := decoder.Decode(&pkg); err != nil {
		return nil, fmt.Errorf("failed to gob decode package: %w", err)
	}
	return &pkg, nil
}
