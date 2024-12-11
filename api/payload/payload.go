package payload

const (
	Json   Format = "application/json"
	Golang Format = "runtime/go"
	Lua    Format = "runtime/lua"
	String Format = "text/plain"
	Bytes  Format = "application/octet-stream"
)

type (
	Format string

	Payload interface {
		Format() Format
		Data() any
	}

	Transcoder interface {
		Transcode(Payload, Format) (Payload, error)
	}

	Marshaller interface {
		Marshal(v interface{}) (Payload, error)
	}

	Unmarshaler interface {
		Unmarshal(Payload, v interface{}) error
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
