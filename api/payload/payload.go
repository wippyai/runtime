// Package payload provides abstractions for handling different data formats and conversions.
package payload

// Format constants for supported payload formats.
const (
	JSON     Format = "json/plain"
	YAML     Format = "yaml/plain"
	String   Format = "text/plain"
	Golang   Format = "golang/any"
	Lua      Format = "lua/any"
	Bytes    Format = "application/octet-stream"
	GoError  Format = "golang/error"
	MsgPack  Format = "application/msgpack"
	Terminal Format = "channel/terminal" // Signals channel should close after this message
)

type (
	// Format represents the data format of a payload.
	Format string

	// Payloads is a slice of Payload objects.
	Payloads = []Payload

	// Payload represents data with an associated format.
	Payload interface {
		// Format returns the format of the payload.
		Format() Format

		// Data returns the raw data of the payload.
		Data() any
	}

	// Unmarshaler unmarshals a payload into a Go data structure.
	Unmarshaler interface {
		// Unmarshal unmarshals the payload into v.
		Unmarshal(Payload, interface{}) error
	}

	// Transcoder converts between payload formats and unmarshals payloads.
	Transcoder interface {
		Unmarshaler

		// Transcode converts a payload to the specified target format.
		Transcode(Payload, Format) (Payload, error)
	}

	// TranscoderRegister registers transcoders and unmarshalers.
	TranscoderRegister interface {
		// RegisterTranscoder registers a transcoder for converting between formats.
		RegisterTranscoder(from, to Format, weight int, tt FormatTranscoder)

		// RegisterUnmarshaler registers an unmarshaler for a specific format.
		RegisterUnmarshaler(from Format, unmarshaler Unmarshaler)
	}

	// FormatTranscoder transcodes a payload from one format to another.
	FormatTranscoder interface {
		// Transcode converts the payload to a different format.
		Transcode(Payload) (Payload, error)
	}
)

// payload is a concrete implementation of the Payload interface.
type payload struct {
	data   any
	format Format
}

// Data returns the raw data of the payload.
func (p payload) Data() any {
	return p.data
}

// Format returns the format of the payload.
func (p payload) Format() Format {
	return p.format
}

// NewPayload creates a new payload with the given data and format.
func NewPayload(data any, format Format) Payload {
	return payload{data: data, format: format}
}

// New creates a new payload with the given data and assumes the Golang format.
func New(data any) Payload {
	return NewPayload(data, Golang)
}

// NewString creates a new payload with the given string data and the String format.
func NewString(data string) Payload {
	return NewPayload(data, String)
}

// NewError creates a new payload wrapping a Go error with the GoError format.
func NewError(data error) Payload {
	return NewPayload(data, GoError)
}

// terminalPayload is a singleton for terminal signals.
var terminalPayload = payload{format: Terminal}

// NewTerminal returns a terminal payload that signals channel closure.
func NewTerminal() Payload {
	return terminalPayload
}

// IsTerminal returns true if the payload is a terminal signal.
func IsTerminal(p Payload) bool {
	return p != nil && p.Format() == Terminal
}
