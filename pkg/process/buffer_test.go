package process

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test message implementation
type testMessage struct {
	kind    string
	from    string
	payload string
}

func (m testMessage) Kind() string { return m.kind }

// Basic write/read test
func TestBufferBasicOperations(t *testing.T) {
	buf := NewBuffer()

	// Write messages
	buf.Write(
		testMessage{kind: "test", from: "1", payload: "a"},
		testMessage{kind: "test", from: "1", payload: "b"},
		testMessage{kind: "other", from: "1", payload: "c"},
	)

	if buf.Len() != 3 {
		t.Errorf("Expected length 3, got %d", buf.Len())
	}

	// Read messages
	msgs := buf.Read()
	if len(msgs) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(msgs))
	}

	if buf.Len() != 0 {
		t.Errorf("Expected length 0 after read, got %d", buf.Len())
	}
}

// Test message ordering
func TestBufferOrdering(t *testing.T) {
	buf := NewBuffer()

	expected := []testMessage{
		{kind: "test", payload: "1"},
		{kind: "test", payload: "2"},
		{kind: "test", payload: "3"},
		{kind: "test", payload: "4"},
	}

	// Write in batches
	buf.Write(expected[0], expected[1])
	buf.Write(expected[2], expected[3])

	// Read all messages
	msgs := buf.Read()

	if len(msgs) != len(expected) {
		t.Fatalf("Expected %d messages, got %d", len(expected), len(msgs))
	}

	// Verify order
	for i, msg := range msgs {
		got := msg.(testMessage)
		if got.payload != expected[i].payload {
			t.Errorf("Message %d: expected payload %s, got %s",
				i, expected[i].payload, got.payload)
		}
	}
}

// Test concurrent writes
func TestBufferConcurrentWrites(t *testing.T) {
	buf := NewBuffer()
	const numGoroutines = 10
	const messagesPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			msgs := make([]Message, messagesPerGoroutine)
			for j := 0; j < messagesPerGoroutine; j++ {
				msgs[j] = testMessage{
					kind:    "test",
					from:    string(rune('A' + id)),
					payload: string(rune('0' + j%10)),
				}
			}
			buf.Write(msgs...)
		}(i)
	}

	wg.Wait()

	// Read all messages
	messages := buf.Read()
	expectedTotal := numGoroutines * messagesPerGoroutine

	if len(messages) != expectedTotal {
		t.Errorf("Expected total messages %d, got %d", expectedTotal, len(messages))
	}

	if buf.Len() != 0 {
		t.Errorf("Expected empty buffer after read, got length %d", buf.Len())
	}
}

// Test interleaved reads and writes
func TestBufferInterleavedOps(t *testing.T) {
	buf := NewBuffer()
	const numWriters = 5
	const messagesPerWriter = 100

	// Counter for received messages
	var received atomic.Int64

	// Start writers
	var wg sync.WaitGroup
	wg.Add(numWriters)

	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerWriter; j++ {
				msg := testMessage{
					kind:    "test",
					from:    string(rune('A' + id)),
					payload: string(rune('0' + j%10)),
				}
				buf.Write(msg)
				time.Sleep(time.Microsecond) // Simulate some work
			}
		}(i)
	}

	// Reader goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			msgs := buf.Read()
			if len(msgs) > 0 {
				received.Add(int64(len(msgs)))
			}
			if received.Load() >= int64(numWriters*messagesPerWriter) {
				return
			}
			time.Sleep(time.Millisecond) // Simulate reader processing
		}
	}()

	// Wait for all writers
	wg.Wait()
	<-done

	expectedTotal := numWriters * messagesPerWriter
	total := received.Load()

	if total != int64(expectedTotal) {
		t.Errorf("Expected to receive %d messages, got %d", expectedTotal, total)
	}
}

// Test stress with rapid writes and reads
func TestBufferStress(t *testing.T) {
	buf := NewBuffer()
	const duration = 2 * time.Second
	const numWriters = 5

	start := time.Now()
	var writeCount atomic.Int64
	var readCount atomic.Int64

	// Start writers
	var wg sync.WaitGroup
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for time.Since(start) < duration {
				msg := testMessage{
					kind:    "test",
					from:    string(rune('A' + id)),
					payload: "data",
				}
				buf.Write(msg)
				writeCount.Add(1)
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Start reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Since(start) < duration {
			msgs := buf.Read()
			if msgs != nil {
				readCount.Add(int64(len(msgs)))
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	// Allow some time for final processing
	time.Sleep(10 * time.Millisecond)

	// Final read attempts to ensure most messages are processed
	for i := 0; i < 5; i++ {
		msgs := buf.Read()
		if msgs != nil {
			readCount.Add(int64(len(msgs)))
		}
		time.Sleep(time.Millisecond)
	}

	writes := writeCount.Load()
	reads := readCount.Load()
	remaining := int64(buf.Len())

	t.Logf("Total writes: %d, reads: %d, remaining: %d", writes, reads, remaining)

	// Check that we're within reasonable bounds (0.1% tolerance)
	if float64(writes-reads-remaining)/float64(writes) > 0.001 {
		t.Errorf("Message accounting error: wrote %d, read %d, remaining %d, missing %d",
			writes, reads, remaining, writes-reads-remaining)
	}
}

// Benchmark sequential writes
func BenchmarkBufferWrite(b *testing.B) {
	buf := NewBuffer()
	msg := testMessage{kind: "test", payload: "data"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(msg)
	}
}

// Benchmark batch writes
func BenchmarkBufferBatchWrite(b *testing.B) {
	buf := NewBuffer()
	batch := make([]Message, 100)
	for i := range batch {
		batch[i] = testMessage{kind: "test", payload: "data"}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(batch...)
	}
}

// Benchmark concurrent writes
func BenchmarkBufferConcurrentWrite(b *testing.B) {
	buf := NewBuffer()
	msg := testMessage{kind: "test", payload: "data"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf.Write(msg)
		}
	})
}

// Benchmark read operations
func BenchmarkBufferRead(b *testing.B) {
	buf := NewBuffer()

	// Pre-fill buffer
	batch := make([]Message, 1000)
	for i := range batch {
		batch[i] = testMessage{kind: "test", payload: "data"}
	}
	buf.Write(batch...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Read()
	}
}
