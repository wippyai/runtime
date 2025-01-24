package native

type reader struct {
	ch <-chan []byte
}

func newReader(ch <-chan []byte) *reader {
	return &reader{
		ch: ch,
	}
}

func (r *reader) Read(p []byte) (n int, err error) {
	select {
	case buf, ok := <-r.ch:
		if !ok {
			return 0, nil
		}
		return copy(p, buf), nil
	}
}

func (r *reader) Close() error {
	return nil
}
