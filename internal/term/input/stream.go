// SPDX-License-Identifier: MPL-2.0

package input

import (
	"context"
	"io"
)

// ReadSize is the size of one read from the input source.
const ReadSize = 4 * 1024

// Stream reads r until it fails or ctx is done and emits decoded events in
// order. Events produced after ctx is done are dropped.
//
// A read that ends in a bare ESC emits the Escape key. When r fails, the
// pending escape is resolved and the read error is returned, io.EOF included,
// after every event decoded from the data returned with it has been emitted.
// A ctx-initiated stop returns nil.
func Stream(ctx context.Context, r io.Reader, emit func(Event)) error {
	deliver := func(ev Event) {
		if ctx.Err() == nil {
			emit(ev)
		}
	}
	p := NewParser()
	buf := make([]byte, ReadSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			p.Feed(buf[:n], deliver)
			p.ResolveEscape(deliver)
		}
		if err != nil {
			p.Flush(deliver)
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}
