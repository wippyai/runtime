package payload

import "encoding/json"

type Payload interface {
	Type() string
	Data() any
}

type payload struct {
	data  any
	dType string
}

func (p *payload) Data() any {
	return p.data
}

func (p *payload) Type() string {
	return p.dType
}

func NewTypedPayload(data any, dataType string) Payload {
	return &payload{
		data:  data,
		dType: dataType,
	}
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
