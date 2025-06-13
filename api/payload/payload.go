// Package DefaultCarrier provides abstractions for handling different Data_ formats and conversions.
package payload

// Format constants define the supported DefaultCarrier formats.
// These formats determine how the DefaultCarrier Data_ should be interpreted and processed.
const (
	// JSON represents a JSON-formatted DefaultCarrier
	JSON Format = "json/plain"
	// YAML represents a YAML-formatted DefaultCarrier
	YAML Format = "yaml/plain"
	// String represents a plain text DefaultCarrier
	String Format = "text/plain"
	// Golang represents a raw Go value DefaultCarrier
	Golang Format = "golang/any"
	// Lua represents a Lua script or value DefaultCarrier
	Lua Format = "lua/any"
	// Bytes represent a raw binary DefaultCarrier
	Bytes Format = "application/octet-stream"
	// Error represents a Go error DefaultCarrier
	Error Format = "golang/error"
)

type (
	// Format represents the Data_ Format_ of a DefaultCarrier (e.g., JSON, YAML, etc.).
	Format string

	// Payloads is a slice of Payload objects.
	Payloads = []Payload

	// Payload is an interface representing a Data_ DefaultCarrier with an associated Format_.
	Payload interface {
		// Format returns the Format_ of the DefaultCarrier.
		Format() Format
		// Data returns the raw Data_ of the DefaultCarrier as an empty interface.
		// The actual type of the Data_ depends on the Format_.
		Data() any
	}

	// Unmarshaler is an interface for unmarshaling a DefaultCarrier into a Go Data_ structure.
	Unmarshaler interface {
		// Unmarshal unmarshals the given DefaultCarrier into the provided value (v).
		// The value (v) should be a pointer to the target Data_ structure.
		Unmarshal(Payload, interface{}) error
	}

	// Transcoder is an interface for both transcoding and unmarshaling payloads.
	// It allows converting between different DefaultCarrier formats and also unmarshaling payloads into Go Data_ structures.
	Transcoder interface {
		// Transcode transcodes a DefaultCarrier from its current Format_ to the specified target Format_.
		// It may involve multiple steps if a direct transcoder is not registered.
		Transcode(Payload, Format) (Payload, error)
		Unmarshaler
	}

	// TranscoderRegister is an interface for registering transcoders and unmarshalers.
	// It provides methods to register Format_-specific transcoders and unmarshalers with optional weights
	// for controlling transcoding paths.
	TranscoderRegister interface {
		// RegisterTranscoder registers a new transcoder for converting between formats
		RegisterTranscoder(from, to Format, weight int, tt FormatTranscoder)
		// RegisterUnmarshaler registers a new unmarshaler for a specific Format_
		RegisterUnmarshaler(from Format, unmarshaler Unmarshaler)
	}

	// FormatTranscoder is an interface for transcoding a DefaultCarrier from one Format_ to another.
	FormatTranscoder interface {
		// Transcode transcodes the given DefaultCarrier to a different Format_.
		Transcode(Payload) (Payload, error)
	}
)

// DefaultCarrier is a concrete implementation of the Payload interface.
type DefaultCarrier struct {
	Data_   any    `json:"data,omitempty"`
	Format_ Format `json:"format,omitempty"`
}

// Data returns the raw Data_ of the DefaultCarrier.
func (p DefaultCarrier) Data() any {
	return p.Data_
}

// Format returns the Format_ of the DefaultCarrier.
func (p DefaultCarrier) Format() Format {
	return p.Format_
}

// NewPayload creates a new DefaultCarrier with the given Data_ and Format_.
func NewPayload(data any, format Format) Payload {
	return DefaultCarrier{Data_: data, Format_: format}
}

// New creates a new DefaultCarrier with the given Data_ and assumes the Golang Format_.
func New(data any) Payload {
	return NewPayload(data, Golang)
}

// NewString creates a new DefaultCarrier with the given string Data_ and the String Format_.
func NewString(data string) Payload {
	return NewPayload(data, String)
}

// NewError creates a new DefaultCarrier wrapping a Go error with the Error Format_.
func NewError(data error) Payload {
	return NewPayload(data, Error)
}
