// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func testStreamOwner(t *testing.T, data []byte) (*retainedFile, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openRetainedTestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newRetainedFile(f)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.release)
	return owner, path
}
func awaitFileReady(t *testing.T, s interface{ Subscribe() preview2.Pollable }) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p := s.Subscribe()
	defer p.Drop()
	p.Block(ctx)
	if ctx.Err() != nil {
		t.Fatal("file stream readiness timed out")
	}
}
func awaitFilePump(t *testing.T, s *fileStream) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		t.Fatal("file stream worker did not stop")
	}
}

func TestFileStreamAdmissionBeforeBufferAllocation(t *testing.T) {
	owner, _ := testStreamOwner(t, make([]byte, 4<<20))
	buffers := preview2.NewHostBufferBudget(1024)
	if stream, err := newFileInputStreamResource(owner, 0, buffers); !errors.Is(err, preview2.ErrHostBufferLimit) || stream != nil {
		t.Fatalf("input admission=%v,%v", stream, err)
	}
	if stream, err := newFileOutputStreamResource(owner, 0, false, buffers); !errors.Is(err, preview2.ErrHostBufferLimit) || stream != nil {
		t.Fatalf("output admission=%v,%v", stream, err)
	}
	if usage := buffers.Usage(); usage.Used != 0 || usage.Peak != 0 {
		t.Fatalf("failed admission allocated: %+v", usage)
	}
}
func TestFileInputStreamBoundedAndRetainsDescriptorIdentity(t *testing.T) {
	expected := bytes.Repeat([]byte("original"), 1<<17)
	owner, path := testStreamOwner(t, expected)
	buffers := preview2.NewHostBufferBudget(fileStreamBufferBytes)
	stream, err := newFileInputStreamResource(owner, 3, buffers)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Drop()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for {
		awaitFileReady(t, stream)
		data, err := stream.Read(1 << 30)
		if len(data) > fileStreamBufferBytes {
			t.Fatalf("unbounded output %d", len(data))
		}
		if err != nil {
			var se *preview2.StreamError
			if !errors.As(err, &se) || !se.Closed {
				t.Fatal(err)
			}
			break
		}
		got = append(got, data...)
	}
	if !bytes.Equal(got, expected[3:]) {
		t.Fatalf("wrong retained-file content: got %d bytes", len(got))
	}
	if usage := buffers.Usage(); usage.Used != fileStreamBufferBytes || usage.Peak != fileStreamBufferBytes {
		t.Fatalf("buffer charge=%+v", usage)
	}
	stream.Drop()
	awaitFilePump(t, stream.fileStream)
	if usage := buffers.Usage(); usage.Used != 0 {
		t.Fatalf("buffer retained after drop: %+v", usage)
	}
}
func TestFileOutputStreamFlushAndIndependentOffsets(t *testing.T) {
	owner, path := testStreamOwner(t, []byte("abcdefgh"))
	buffers := preview2.NewHostBufferBudget(2 * fileStreamBufferBytes)
	a, err := newFileOutputStreamResource(owner, 1, false, buffers)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Drop()
	b, err := newFileOutputStreamResource(owner, 5, false, buffers)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Drop()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Write([]byte("XY")); err != nil {
		t.Fatal(err)
	}
	if err := b.Write([]byte("Z")); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := b.Flush(); err != nil {
		t.Fatal(err)
	}
	awaitFileReady(t, a)
	awaitFileReady(t, b)
	if _, err := a.CheckWrite(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CheckWrite(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path + ".old")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "aXYdeZgh" {
		t.Fatalf("offset writes=%q", data)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "replacement" {
		t.Fatal("write reopened replacement pathname")
	}
	a.Drop()
	b.Drop()
	awaitFilePump(t, a.fileStream)
	awaitFilePump(t, b.fileStream)
	if buffers.Usage().Used != 0 {
		t.Fatal("output buffers not released")
	}
}

type blockedFileRead struct {
	*os.File
	entered chan struct{}
	unblock chan struct{}
	closes  atomic.Int32
}

func (f *blockedFileRead) ReadAt(_ []byte, _ int64) (int, error) {
	close(f.entered)
	<-f.unblock
	return 0, io.EOF
}
func (f *blockedFileRead) WriteAt(p []byte, offset int64) (int, error) {
	close(f.entered)
	<-f.unblock
	return f.File.WriteAt(p, offset)
}
func (f *blockedFileRead) Close() error { f.closes.Add(1); return f.File.Close() }

func TestFileStreamDropRetainsInFlightBufferCharge(t *testing.T) {
	for _, output := range []bool{false, true} {
		name := "read"
		if output {
			name = "write"
		}
		t.Run(name, func(t *testing.T) { testFileStreamDropRetainsInFlightBufferCharge(t, output) })
	}
}

func testFileStreamDropRetainsInFlightBufferCharge(t *testing.T, output bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockedFileRead{File: file, entered: make(chan struct{}), unblock: make(chan struct{})}
	owner, err := newRetainedFile(blocked)
	if err != nil {
		t.Fatal(err)
	}
	buffers := preview2.NewHostBufferBudget(fileStreamBufferBytes)
	var stream *fileStream
	if output {
		writer, openErr := newFileOutputStreamResource(owner, 0, false, buffers)
		if openErr != nil {
			t.Fatal(openErr)
		}
		stream = writer.fileStream
		if writeErr := writer.Write([]byte("retained output")); writeErr != nil {
			t.Fatal(writeErr)
		}
	} else {
		reader, openErr := newFileInputStreamResource(owner, 0, buffers)
		if openErr != nil {
			t.Fatal(openErr)
		}
		stream = reader.fileStream
	}
	defer stream.Drop()
	// Ensure a failed assertion cannot leave an audit/test worker blocked.
	var unblock sync.Once
	defer unblock.Do(func() { close(blocked.unblock) })
	select {
	case <-blocked.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("I/O not started")
	}
	released := make(chan struct{})
	go func() { owner.release(); stream.Drop(); close(released) }()
	select {
	case <-released:
	case <-time.After(3 * time.Second):
		t.Fatal("drop blocked on in-flight I/O")
	}
	if !stream.Ready() {
		t.Fatal("drop did not wake pollers")
	}
	if buffers.Usage().Used != fileStreamBufferBytes {
		t.Fatal("freed buffer charge while I/O owns backing storage")
	}
	if blocked.closes.Load() != 0 {
		t.Fatal("closed shared file while I/O owns it")
	}
	unblock.Do(func() { close(blocked.unblock) })
	awaitFilePump(t, stream)
	if buffers.Usage().Used != 0 || blocked.closes.Load() != 1 {
		t.Fatalf("cleanup: usage=%+v closes=%d", buffers.Usage(), blocked.closes.Load())
	}
}

func TestFileAppendRequiresAtomicBackingCapability(t *testing.T) {
	owner, path := testStreamOwner(t, []byte("unchanged"))
	buffers := preview2.NewHostBufferBudget(fileStreamBufferBytes)
	stream, err := newFileOutputStreamResource(owner, 0, true, buffers)
	if !errors.Is(err, errors.ErrUnsupported) || stream != nil {
		t.Fatalf("append without atomic backing capability: %v, %v", stream, err)
	}
	if usage := buffers.Usage(); usage.Used != 0 || usage.Peak != 0 {
		t.Fatalf("unsupported append admitted a buffer: %+v", usage)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "unchanged" {
		t.Fatalf("unsupported append modified content: %q", data)
	}
}

// Admission can fail after a stream worker has acquired its buffer and file.
// The rejected handle must unwind those leases without closing its descriptor.
func TestFileStreamHandleExhaustionReleasesWorkerLeases(t *testing.T) {
	for _, output := range []bool{false, true} {
		name := "read"
		if output {
			name = "write"
		}
		t.Run(name, func(t *testing.T) {
			owner, _ := testStreamOwner(t, []byte("retained"))
			buffers := preview2.NewHostBufferBudget(fileStreamBufferBytes)
			table := preview2.NewResourceTableWithBudgets(1, preview2.NewSocketBudget(1), buffers)
			defer table.Close()
			handle := table.Add(&descriptorResource{file: owner, readable: true, writable: true})
			host := NewTypesHost(table)
			var trap any
			func() {
				defer func() { trap = recover() }()
				if output {
					_, err := host.MethodDescriptorWriteViaStream(context.Background(), handle, 0)
					if err != nil {
						t.Errorf("unexpected pre-admission error: %v", err)
					}
				} else {
					_, err := host.MethodDescriptorReadViaStream(context.Background(), handle, 0)
					if err != nil {
						t.Errorf("unexpected pre-admission error: %v", err)
					}
				}
			}()
			trapErr, ok := trap.(error)
			if !ok || !errors.Is(trapErr, preview2.ErrResourceLimit) {
				t.Fatalf("handle-exhaustion trap: %v", trap)
			}
			deadline := time.Now().Add(3 * time.Second)
			for buffers.Usage().Used != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if used := buffers.Usage().Used; used != 0 {
				t.Fatalf("rejected stream retained %d buffer bytes", used)
			}
			if _, err := owner.stat(); err != nil {
				t.Fatalf("rejected child closed parent descriptor: %v", err)
			}
			table.Close()
			if _, err := owner.stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("descriptor close left a worker lease: %v", err)
			}
		})
	}
}
