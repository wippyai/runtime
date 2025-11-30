package io

import (
	"testing"

	"github.com/wippyai/runtime/runtime/wasm/resource"
)

func TestStreamsHost(t *testing.T) {
	t.Run("creates with shared resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewStreamsHost(res)

		if host.Resources() != res {
			t.Error("expected same resources instance")
		}
	})

	t.Run("info returns correct namespace", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewStreamsHost(res)
		info := host.Info()

		if info.Namespace != StreamsNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, StreamsNamespace)
		}
	})

	t.Run("register returns functions and yield types", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewStreamsHost(res)
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}

		expectedFuncs := []string{
			"[method]input-stream.read",
			"[method]input-stream.blocking-read",
			"[method]output-stream.write",
			"[method]output-stream.flush",
			"[resource-drop]input-stream",
			"[resource-drop]output-stream",
		}

		for _, name := range expectedFuncs {
			if _, ok := reg.Functions[name]; !ok {
				t.Errorf("missing function: %s", name)
			}
		}

		if len(reg.YieldTypes) == 0 {
			t.Error("expected yield types")
		}
	})

	t.Run("uses shared input streams", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewStreamsHost(res)

		stream := &resource.InputStream{StreamID: 42}
		handle := res.InputStreams().Insert(stream)

		got, ok := res.InputStreams().Get(handle)
		if !ok {
			t.Fatal("expected stream")
		}
		if got.StreamID != 42 {
			t.Errorf("streamID = %d, want 42", got.StreamID)
		}

		// Resources should be in shared table
		if res.Len() != 1 {
			t.Errorf("resource count = %d, want 1", res.Len())
		}

		_ = host // used for context
	})

	t.Run("uses shared output streams", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewStreamsHost(res)

		stream := &resource.OutputStream{StreamID: 100}
		handle := res.OutputStreams().Insert(stream)

		got, ok := res.OutputStreams().Get(handle)
		if !ok {
			t.Fatal("expected stream")
		}
		if got.StreamID != 100 {
			t.Errorf("streamID = %d, want 100", got.StreamID)
		}

		_ = host
	})

	t.Run("dropper called on remove", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		stream := &resource.InputStream{StreamID: 1, Closed: false}
		handle := res.InputStreams().Insert(stream)

		res.Table().Remove(handle)

		if !stream.Closed {
			t.Error("expected Closed=true after remove")
		}
	})

	t.Run("close releases all streams", func(t *testing.T) {
		res := resource.NewInstanceResources()
		host := NewStreamsHost(res)

		in := &resource.InputStream{StreamID: 1}
		out := &resource.OutputStream{StreamID: 2}
		res.InputStreams().Insert(in)
		res.OutputStreams().Insert(out)

		if res.Len() != 2 {
			t.Errorf("resource count = %d, want 2", res.Len())
		}

		res.Close()

		if !in.Closed {
			t.Error("expected input stream closed")
		}
		if !out.Closed {
			t.Error("expected output stream closed")
		}
		if res.Len() != 0 {
			t.Errorf("resource count after close = %d, want 0", res.Len())
		}

		_ = host
	})
}

func BenchmarkStreamsWithSharedResources(b *testing.B) {
	res := resource.NewInstanceResources()
	defer res.Close()

	stream := &resource.InputStream{StreamID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := res.InputStreams().Insert(stream)
		res.InputStreams().Get(h)
		res.Table().Remove(h)
	}
}
