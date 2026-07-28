// SPDX-License-Identifier: MPL-2.0

package io

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type testInputStream struct {
	data      []byte
	readErr   error
	readCalls []uint64
	dropCalls int
	dropped   bool
}

func (s *testInputStream) Type() preview2.ResourceType { return preview2.ResourceInputStream }
func (s *testInputStream) Drop() {
	if !s.dropped {
		s.dropCalls++
		s.dropped = true
	}
}
func (s *testInputStream) Read(length uint64) ([]byte, error) {
	s.readCalls = append(s.readCalls, length)
	return s.data, s.readErr
}

type testOutputStream struct {
	writes     [][]byte
	writeErr   error
	flushCalls int
	dropCalls  int
	dropped    bool
}

func (s *testOutputStream) Type() preview2.ResourceType { return preview2.ResourceOutputStream }
func (s *testOutputStream) Drop() {
	if !s.dropped {
		s.dropCalls++
		s.dropped = true
	}
}
func (s *testOutputStream) Write(data []byte) error {
	s.writes = append(s.writes, append([]byte(nil), data...))
	return s.writeErr
}
func (s *testOutputStream) Flush() error {
	s.flushCalls++
	return nil
}

func TestR01BlockingWriteStopsBeforeFlush(t *testing.T) {
	resources := preview2.NewResourceTable()
	wantErr := &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: 17}
	output := &testOutputStream{writeErr: wantErr}
	handle := resources.Add(output)

	gotErr := NewStreamsHost(resources).MethodOutputStreamBlockingWriteAndFlush(context.Background(), handle, []byte("payload"))
	if gotErr != wantErr {
		t.Fatalf("blocking write error = %p %#v, want exact error %p %#v", gotErr, gotErr, wantErr, wantErr)
	}
	if output.flushCalls != 0 {
		t.Fatalf("Flush calls = %d, want 0 after write failure", output.flushCalls)
	}
}

func TestR02SpliceReportsShortRead(t *testing.T) {
	resources := preview2.NewResourceTable()
	input := &testInputStream{data: []byte("abc")}
	output := &testOutputStream{}
	inputHandle := resources.Add(input)
	outputHandle := resources.Add(output)

	got, gotErr := NewStreamsHost(resources).MethodOutputStreamSplice(context.Background(), outputHandle, inputHandle, 8)
	if gotErr != nil {
		t.Fatalf("splice error = %v", gotErr)
	}
	if got != 3 {
		t.Fatalf("splice count = %d, want 3", got)
	}
	if len(output.writes) != 1 || string(output.writes[0]) != "abc" {
		t.Fatalf("writes = %#v, want one write of abc", output.writes)
	}
}

func TestR03SpliceReadFailureWritesNothing(t *testing.T) {
	resources := preview2.NewResourceTable()
	wantErr := &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: 23}
	input := &testInputStream{readErr: wantErr}
	output := &testOutputStream{}
	inputHandle := resources.Add(input)
	outputHandle := resources.Add(output)

	got, gotErr := NewStreamsHost(resources).MethodOutputStreamSplice(context.Background(), outputHandle, inputHandle, 8)
	if got != 0 {
		t.Fatalf("splice count = %d, want 0", got)
	}
	if gotErr != wantErr {
		t.Fatalf("splice error = %p %#v, want exact error %p %#v", gotErr, gotErr, wantErr, wantErr)
	}
	if len(output.writes) != 0 {
		t.Fatalf("Write calls = %d, want 0 after read failure", len(output.writes))
	}
}

func TestR04WriteZeroesRejectsOversizeWithoutAllocation(t *testing.T) {
	resources := preview2.NewResourceTable()
	output := &testOutputStream{}
	handle := resources.Add(output)

	gotErr := NewStreamsHost(resources).MethodOutputStreamWriteZeroes(context.Background(), handle, preview2.MaxAllocationSize+1)
	if gotErr == nil || !gotErr.LastOpFailed {
		t.Fatalf("write-zeroes error = %#v, want last-operation-failed", gotErr)
	}
	if len(output.writes) != 0 {
		t.Fatalf("Write calls = %d, want 0 for oversize request", len(output.writes))
	}
}

func TestR05InputReadForwardsBoundedLength(t *testing.T) {
	resources := preview2.NewResourceTable()
	input := &testInputStream{}
	handle := resources.Add(input)
	host := NewStreamsHost(resources)

	if _, gotErr := host.MethodInputStreamRead(context.Background(), handle, preview2.MaxAllocationSize); gotErr != nil {
		t.Fatalf("maximum-size read error = %v", gotErr)
	}
	if len(input.readCalls) != 1 || input.readCalls[0] != preview2.MaxAllocationSize {
		t.Fatalf("Read lengths = %v, want [%d]", input.readCalls, preview2.MaxAllocationSize)
	}

	if _, gotErr := host.MethodInputStreamRead(context.Background(), handle, preview2.MaxAllocationSize+1); gotErr == nil || !gotErr.LastOpFailed {
		t.Fatalf("oversize read error = %#v, want last-operation-failed", gotErr)
	}
	if len(input.readCalls) != 1 {
		t.Fatalf("Read calls = %d, want oversize rejected before second call", len(input.readCalls))
	}
}

func TestR06StreamDropCleanupOnce(t *testing.T) {
	resources := preview2.NewResourceTable()
	input := &testInputStream{}
	output := &testOutputStream{}
	inputHandle := resources.Add(input)
	outputHandle := resources.Add(output)
	host := NewStreamsHost(resources)

	host.ResourceDropInputStream(context.Background(), inputHandle)
	host.ResourceDropInputStream(context.Background(), inputHandle)
	host.ResourceDropOutputStream(context.Background(), outputHandle)
	host.ResourceDropOutputStream(context.Background(), outputHandle)

	if input.dropCalls != 1 || output.dropCalls != 1 {
		t.Fatalf("Drop calls = input:%d output:%d, want one each", input.dropCalls, output.dropCalls)
	}
	if _, gotErr := host.MethodInputStreamRead(context.Background(), inputHandle, 1); gotErr == nil || !gotErr.Closed {
		t.Fatalf("read after drop error = %#v, want closed", gotErr)
	}
	if gotErr := host.MethodOutputStreamWrite(context.Background(), outputHandle, []byte("x")); gotErr == nil || !gotErr.Closed {
		t.Fatalf("write after drop error = %#v, want closed", gotErr)
	}
}
