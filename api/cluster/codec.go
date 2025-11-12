package cluster

import "github.com/ponyruntime/pony/api/pubsub"

// MessageCodec handles encoding and decoding of pubsub packages for
// transmission over the network between cluster nodes.
type MessageCodec interface {
	// Encode serializes a pubsub.Package into a byte slice suitable for
	// network transmission.
	Encode(pkg *pubsub.Package) ([]byte, error)

	// Decode deserializes a byte slice back into a pubsub.Package.
	Decode(data []byte) (*pubsub.Package, error)
}
