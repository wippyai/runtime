// SPDX-License-Identifier: MPL-2.0

// Package payload provides abstractions for handling different data formats and conversions.
package payload

// Payload format constants.
const (
	JSON     Format = "json/plain"
	YAML     Format = "yaml/plain"
	String   Format = "text/plain"
	Golang   Format = "golang/any"
	Lua      Format = "lua/any"
	Bytes    Format = "application/octet-stream"
	GoError  Format = "golang/error"
	MsgPack  Format = "application/msgpack"
	Terminal Format = "channel/terminal"
)

type (
	// Format represents the data format of a payload.
	Format = string

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

	// ContextFormatTranscoder optionally receives transcoding context for a step.
	// Implement this when a transcoder needs access to the active parent transcoder
	// (for nested payload conversion) or step metadata.
	ContextFormatTranscoder interface {
		// TranscodeWith converts payload using contextual metadata for this step.
		TranscodeWith(*TranscodeContext, Payload) (Payload, error)
	}

	// TranscodeOptions carries optional hints for future transcoding features.
	TranscodeOptions struct {
		Extras     map[string]any
		TypeHint   string
		TargetKind string
	}

	// TranscodeContext describes the current transcoding step.
	TranscodeContext struct {
		Parent  Transcoder
		Options *TranscodeOptions
		From    Format
		To      Format
		Depth   int
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

// SnapshotData returns a transport-safe copy of common mutable payload data.
// Payloads frequently carry map[string]any / []any trees assembled by services
// and then fanned out to multiple peers. Encoding those trees directly lets one
// goroutine iterate a map while another goroutine mutates or normalizes it. The
// snapshot copies maps, []any slices, and byte buffers recursively; immutable
// scalars and structs are returned by value.
func SnapshotData(data any) any {
	switch v := data.(type) {
	case map[string]any:
		cp := make(map[string]any, len(v))
		for k, val := range v {
			cp[k] = SnapshotData(val)
		}
		return cp
	case map[any]any:
		cp := make(map[any]any, len(v))
		for k, val := range v {
			cp[SnapshotData(k)] = SnapshotData(val)
		}
		return cp
	case []any:
		cp := make([]any, len(v))
		for i, val := range v {
			cp[i] = SnapshotData(val)
		}
		return cp
	case []byte:
		return append([]byte(nil), v...)
	case Payload:
		return Snapshot(v)
	case Payloads:
		return SnapshotAll(v)
	default:
		return data
	}
}

// Snapshot returns a payload with SnapshotData applied to its data.
func Snapshot(p Payload) Payload {
	if p == nil {
		return nil
	}
	return NewPayload(SnapshotData(p.Data()), p.Format())
}

// SnapshotAll returns a new payload slice with each payload snapshot-copied.
func SnapshotAll(in Payloads) Payloads {
	if len(in) == 0 {
		return nil
	}
	out := make(Payloads, len(in))
	for i, p := range in {
		out[i] = Snapshot(p)
	}
	return out
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
