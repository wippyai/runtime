package payload

import "encoding/json"

type Header string

const TypeHeader Header = "content-type"

type Payload interface {
	Type() string
	Data() any
}

type payload struct {
	data    any
	headers map[Header]string
}

func (p *payload) Data() any {
	return p.data
}

func (p *payload) Type() string {
	return p.headers[TypeHeader]
}

func NewTypedPayload(data any, contentType string) Payload {
	p := &payload{
		data:    data,
		headers: make(map[Header]string),
	}

	p.headers[TypeHeader] = contentType

	return p
}

func NewJSON(data json.RawMessage) Payload {
	return NewTypedPayload(data, "application/json")
}

func New(data any) Payload {
	return NewTypedPayload(data, "golang/any")
}

func NewString(data string) Payload {
	return NewTypedPayload(data, "text/plain")
}

func NewBytes(data []byte) Payload {
	return NewTypedPayload(data, "application/octet-stream")
}
