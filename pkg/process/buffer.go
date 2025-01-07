package process

import (
	"sync"
)

type node struct {
	msg  []Message
	next *node
}

type Buffer struct {
	join chan *node

	head *node
	tail *node
	size int64
	mu   sync.RWMutex
}

func NewBuffer() *Buffer {
	b := &Buffer{
		join: make(chan *node, 100),
	}

	go func() {
		for n := range b.join {
			b.mu.Lock()
			b.size += int64(len(n.msg))

			if b.tail == nil {
				b.head = n
				b.tail = n
				b.mu.Unlock()
				break
			}

			tail := b.tail
			tail.next = n
			b.tail = n
			b.mu.Unlock()
		}
	}()

	return b
}

func (b *Buffer) Write(msgs ...Message) {
	if len(msgs) == 0 {
		return
	}

	messageCopy := make([]Message, len(msgs))
	copy(messageCopy, msgs)

	// create new node
	n := &node{msg: messageCopy}
	b.join <- n
}

func (b *Buffer) Read() []Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	var messages []Message

	head := b.head
	if head == nil {
		return messages
	}

	messages = append(messages, head.msg...)

	// make all pending writes visible
	current := head.next

	if current == nil {
		return messages
	}

	// collect all messages
	for current != nil {
		messages = append(messages, current.msg...)
		current = current.next
	}

	// reset buffer
	b.head = nil
	b.tail = nil
	b.size = 0

	return messages
}

func (b *Buffer) Len() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

func (b *Buffer) Close() {
	close(b.join)
}
