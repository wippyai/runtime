// Package cluster provides cluster communication and message encoding.
package cluster

import "github.com/wippyai/runtime/api/relay"

// MessageCodec handles encoding and decoding of relay packages for
// transmission over the network between cluster nodes.
type MessageCodec interface {
	// Encode serializes a relay.Package into a byte slice suitable for
	// network transmission.
	Encode(pkg *relay.Package) ([]byte, error)

	// Decode deserializes a byte slice back into a relay.Package.
	Decode(data []byte) (*relay.Package, error)
}
