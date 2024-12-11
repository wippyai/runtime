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
	Format string

	Payload interface {
		Format() Format
		Data() any
	}

	Unmarshaler interface {
		Unmarshal(Payload, interface{}) error
	}

	Transcoder interface {
		RegisterTranscoder(from, to Format, weight int, tt FormatTranscoder)
		RegisterUnmarshaler(from Format, unmarshaler Unmarshaler)

		Transcode(Payload, Format) (Payload, error)
		Unmarshaler
	}

	FormatTranscoder interface {
		Transcode(Payload) (Payload, error)
	}
)

type payload struct {
	data   any
	format Format
}

func (p *payload) Data() any {
	return p.data
}

func (p *payload) Format() Format {
	return p.format
}

func NewPayload(data any, format Format) Payload {
	return &payload{data: data, format: format}
}

func New(data any) Payload {
	return NewPayload(data, Golang)
}

func NewString(data string) Payload {
	return NewPayload(data, String)
}
