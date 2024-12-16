package payload

const (
	Json   Format = "application/json"
	Yaml   Format = "application/x-yaml"
	Golang Format = "golang/any"
	Lua    Format = "lua/any"
	String Format = "text/plain"
	Bytes  Format = "application/octet-stream"
)

type (
	// Format represents the data format of a payload (e.g., JSON, YAML, etc.).
	Format string

	// Payload is an interface representing a data payload with an associated format.
	Payload interface {
		// Format returns the format of the payload.
		Format() Format
		// Data returns the raw data of the payload as an empty interface.
		// The actual type of the data depends on the format.
		Data() any
	}

	// Unmarshaler is an interface for unmarshaling a payload into a Go data structure.
	Unmarshaler interface {
		// Unmarshal unmarshals the given payload into the provided value (v).
		// The value (v) should be a pointer to the target data structure.
		Unmarshal(Payload, interface{}) error
	}

	// Transcoder is an interface for both transcoding and unmarshaling payloads.
	// It allows converting between different payload formats and also unmarshaling payloads into Go data structures.
	Transcoder interface {
		// RegisterTranscoder registers a FormatTranscoder for converting payloads from one format to another.
		RegisterTranscoder(from, to Format, weight int, tt FormatTranscoder)
		// RegisterUnmarshaler registers an Unmarshaler for a specific format.
		RegisterUnmarshaler(from Format, unmarshaler Unmarshaler)

		// Transcode transcodes a payload from its current format to the specified target format.
		// It may involve multiple steps if a direct transcoder is not registered.
		Transcode(Payload, Format) (Payload, error)
		Unmarshaler
	}

	// FormatTranscoder is an interface for transcoding a payload from one format to another.
	FormatTranscoder interface {
		// Transcode transcodes the given payload to a different format.
		Transcode(Payload) (Payload, error)
	}
)

// payload is a concrete implementation of the Payload interface.
type payload struct {
	data   any
	format Format
}

// Data returns the raw data of the payload.
func (p *payload) Data() any {
	return p.data
}

// Format returns the format of the payload.
func (p *payload) Format() Format {
	return p.format
}

// NewPayload creates a new payload with the given data and format.
func NewPayload(data any, format Format) Payload {
	return &payload{data: data, format: format}
}

// New creates a new payload with the given data and assumes the Golang format.
func New(data any) Payload {
	return NewPayload(data, Golang)
}

// NewString creates a new payload with the given string data and the String format.
func NewString(data string) Payload {
	return NewPayload(data, String)
}
