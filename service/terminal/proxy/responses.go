// SPDX-License-Identifier: MPL-2.0

package proxy

import "sync"

// responseQueueLimit bounds terminal replies awaiting delivery to the child.
// A child that stops reading its input while flooding queries stalls the
// parser here rather than growing memory.
const responseQueueLimit = 64 << 10

// responseQueue carries emulator replies from the output parser to the
// goroutine that writes them to the PTY. Saturation applies backpressure;
// shutdown or loss of the writer must close the queue to release the parser.
type responseQueue struct {
	cond   *sync.Cond
	chunks [][]byte
	mu     sync.Mutex
	size   int
	closed bool
}

func (q *responseQueue) init() { q.cond = sync.NewCond(&q.mu) }

// push enqueues a reply, waiting while the queue is full. Replies pushed after
// close are dropped.
func (q *responseQueue) push(data []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.size >= responseQueueLimit && !q.closed {
		q.cond.Wait()
	}
	if q.closed {
		return
	}
	q.chunks = append(q.chunks, data)
	q.size += len(data)
	q.cond.Broadcast()
}

// next blocks until a reply is available. Replies queued before close are
// still delivered; it reports false once the queue is closed and drained.
func (q *responseQueue) next() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.chunks) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.chunks) == 0 {
		return nil, false
	}
	data := q.chunks[0]
	q.chunks = q.chunks[1:]
	q.size -= len(data)
	q.cond.Broadcast()
	return data, true
}

func (q *responseQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}
