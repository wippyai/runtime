package payload

// todo: define it

type (
	// Transcoder is an interface that defines the methods for encoding and decoding payloads.

	TypedTranscoder interface {
		Encode(Payload) ([]byte, error)
		Decode([]byte) (Payload, error)
	}
)
