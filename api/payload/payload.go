// Package payload provides abstractions for handling different data formats and conversions.
package payload

// Format constants define the supported payload formats.
// These formats determine how the payload data should be interpreted and processed.
const (
	// JSON represents a JSON-formatted payload
	JSON Format = "json/plain"
	// YAML represents a YAML-formatted payload
	YAML Format = "yaml/plain"
	// String represents a plain text payload
	String Format = "text/plain"
	// Golang represents a raw Go value payload
	Golang Format = "golang/any"
	// Lua represents a Lua script or value payload
	Lua Format = "lua/any"
	// Bytes represent a raw binary payload
	Bytes Format = "application/octet-stream"
	// Error represents a Go error payload
	Error Format = "golang/error"
)

type (
	// Format represents the data format of a payload (e.g., JSON, YAML, etc.).
	Format string

	// Payloads is a slice of Payload objects.
	Payloads = []Payload

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
		// Transcode transcodes a payload from its current format to the specified target format.
		// It may involve multiple steps if a direct transcoder is not registered.
		Transcode(Payload, Format) (Payload, error)
		Unmarshaler
	}

	// TranscoderRegister is an interface for registering transcoders and unmarshalers.
	// It provides methods to register format-specific transcoders and unmarshalers with optional weights
	// for controlling transcoding paths.
	TranscoderRegister interface {
		// RegisterTranscoder registers a new transcoder for converting between formats
		RegisterTranscoder(from, to Format, weight int, tt FormatTranscoder)
		// RegisterUnmarshaler registers a new unmarshaler for a specific format
		RegisterUnmarshaler(from Format, unmarshaler Unmarshaler)
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

// NewError creates a new payload wrapping a Go error with the Error format.
func NewError(data error) Payload {
	return NewPayload(data, Error)
}
