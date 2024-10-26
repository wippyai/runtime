package payload

type Header string

const TypeHeader Header = "content-type"

type Payload interface {
	Type() string
	Data() []byte
}

type payload struct {
	data    []byte
	headers map[Header]string
}

func (p *payload) Data() []byte {
	return p.data
}

func (p *payload) Type() string {
	return p.headers[TypeHeader]
}

func NewTypedPayload(data []byte, contentType string) Payload {
	p := &payload{
		data:    data,
		headers: make(map[Header]string),
	}

	p.headers[TypeHeader] = contentType

	return p
}

func NewJSON(data []byte) Payload {
	return NewTypedPayload(data, "application/json")
}

func NewString(data string) Payload {
	return NewTypedPayload([]byte(data), "text/plain")
}

func NewBinary(data []byte) Payload {
	return NewTypedPayload(data, "application/octet-stream")
}
