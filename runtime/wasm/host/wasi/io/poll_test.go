package io

import (
	"testing"

	pollapi "github.com/wippyai/runtime/api/dispatcher/poll"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

func TestPollHost(t *testing.T) {
	t.Run("creates with shared resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPollHost(res)

		if host.Resources() != res {
			t.Error("expected same resources instance")
		}
	})

	t.Run("info returns correct namespace", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPollHost(res)
		info := host.Info()

		if info.Namespace != PollNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, PollNamespace)
		}
	})

	t.Run("register returns functions and yield types", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPollHost(res)
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}

		expectedFuncs := []string{
			"poll",
			"[method]pollable.ready",
			"[method]pollable.block",
			"[resource-drop]pollable",
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

	t.Run("uses shared pollables table", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPollHost(res)

		p := &resource.Pollable{SourceID: 42, Ready: false}
		handle := res.Pollables().Insert(p)

		got, ok := res.Pollables().Get(handle)
		if !ok {
			t.Fatal("expected pollable")
		}
		if got.SourceID != 42 {
			t.Errorf("sourceID = %d, want 42", got.SourceID)
		}

		if res.Len() != 1 {
			t.Errorf("resource count = %d, want 1", res.Len())
		}

		_ = host
	})

	t.Run("makePollCmd creates valid command", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewPollHost(res)

		p := &resource.Pollable{SourceID: 100, Ready: false}
		handle := res.Pollables().Insert(p)

		stack := []uint64{uint64(handle), 1}
		cmd := host.makePollCmd(stack)

		pollCmd, ok := cmd.(pollapi.PollCmd)
		if !ok {
			t.Fatalf("expected PollCmd, got %T", cmd)
		}

		if len(pollCmd.Pollables) != 1 {
			t.Errorf("pollables count = %d, want 1", len(pollCmd.Pollables))
		}
		if pollCmd.Pollables[0] != 100 {
			t.Errorf("pollable sourceID = %d, want 100", pollCmd.Pollables[0])
		}
	})

	t.Run("dropper called on remove", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		p := &resource.Pollable{SourceID: 1, Ready: false}
		handle := res.Pollables().Insert(p)

		res.Table().Remove(handle)

		// Pollable has no custom Drop, but table should be empty
		if res.Len() != 0 {
			t.Errorf("resource count = %d, want 0", res.Len())
		}
	})

	t.Run("close releases all pollables", func(t *testing.T) {
		res := resource.NewInstanceResources()
		host := NewPollHost(res)

		res.Pollables().Insert(&resource.Pollable{SourceID: 1})
		res.Pollables().Insert(&resource.Pollable{SourceID: 2})

		if res.Len() != 2 {
			t.Errorf("resource count = %d, want 2", res.Len())
		}

		res.Close()

		if res.Len() != 0 {
			t.Errorf("resource count after close = %d, want 0", res.Len())
		}

		_ = host
	})
}

func BenchmarkPollWithSharedResources(b *testing.B) {
	res := resource.NewInstanceResources()
	defer res.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := resource.AcquirePollable()
		p.SourceID = 1
		h := res.Pollables().Insert(p)
		res.Pollables().Get(h)
		res.Table().Remove(h)
	}
}
