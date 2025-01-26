package native

import (
	"errors"
)

type reader struct {
	ch <-chan []byte
}

func newReader(ch <-chan []byte) *reader {
	return &reader{
		ch: ch,
	}
}

func (r *reader) Read(p []byte) (int, error) {
	select {
	case buf, ok := <-r.ch:
		if !ok {
			return 0, nil
		}
		return copy(p, buf), nil
	default:
		return 0, errors.New("failed to read from the channel")
	}
}

func (r *reader) Close() error {
	return nil
}
