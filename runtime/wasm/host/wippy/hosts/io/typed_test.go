// SPDX-License-Identifier: MPL-2.0

package io

import (
	"context"
	"errors"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestAsHostErrorNilConversionAndIdentity(t *testing.T) {
	if err := asHostError(nil); err != nil {
		t.Fatalf("nil StreamError must convert to nil error, got %T %v", err, err)
	}
	var typedNil *preview2.StreamError
	if err := asHostError(typedNil); err != nil {
		t.Fatalf("typed-nil StreamError must convert to nil error, got %T %v", err, err)
	}
	want := &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: 21}
	got := asHostError(want)
	if got != want { //nolint:errorlint // Conversion must preserve the original error object.
		t.Fatalf("StreamError identity lost: got %p want %p", got, want)
	}
}

func TestTypedStreamWrappers(t *testing.T) {
	resources := preview2.NewResourceTable()
	input := &testInputStream{data: []byte("abc")}
	output := &testOutputStream{}
	inHandle := resources.Add(input)
	outHandle := resources.Add(output)
	host := NewStreamsHost(resources)
	ctx := context.Background()

	data, err := host.typedInputStreamRead(ctx, inHandle, 8)
	if err != nil || string(data) != "abc" {
		t.Fatalf("read data=%q err=%v", data, err)
	}
	n, err := host.typedInputStreamSkip(ctx, inHandle, 8)
	if err != nil || n != 3 {
		t.Fatalf("skip n=%d err=%v", n, err)
	}
	permit, err := host.typedOutputStreamCheckWrite(ctx, outHandle)
	if err != nil || permit != 1024*1024 {
		t.Fatalf("check-write permit=%d err=%v", permit, err)
	}
	if _, err := host.typedOutputStreamWrite(ctx, outHandle, []byte("xy")); err != nil {
		t.Fatalf("write err=%v", err)
	}
	if _, err := host.typedOutputStreamFlush(ctx, outHandle); err != nil {
		t.Fatalf("flush err=%v", err)
	}

	closed, err := host.typedInputStreamRead(ctx, 0xffffffff, 1)
	if closed != nil {
		t.Fatalf("missing stream returned data %v", closed)
	}
	var se *preview2.StreamError
	if !errors.As(err, &se) || se == nil || !se.Closed {
		t.Fatalf("missing stream error = %T %#v", err, err)
	}
}

func TestTypedStreamWrapperErrorIdentity(t *testing.T) {
	resources := preview2.NewResourceTable()
	want := &preview2.StreamError{LastOpFailed: true, LastOpFailedErr: 17}
	output := &testOutputStream{writeErr: want}
	handle := resources.Add(output)
	host := NewStreamsHost(resources)

	_, err := host.typedOutputStreamWrite(context.Background(), handle, []byte("x"))
	if err != want { //nolint:errorlint // The wrapper must preserve the original error object.
		t.Fatalf("write error = %p %#v, want %p %#v", err, err, want, want)
	}
}
