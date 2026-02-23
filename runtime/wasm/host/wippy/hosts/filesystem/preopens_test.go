// SPDX-License-Identifier: MPL-2.0

package filesystem

import (
	"context"
	"testing"
	"testing/fstest"

	fsapi "github.com/wippyai/runtime/api/fs"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestPreopensHost_Empty(t *testing.T) {
	host := NewPreopensHost(preview2.NewResourceTable())
	if got := host.GetDirectories(context.Background()); got != nil {
		t.Fatalf("GetDirectories() = %#v, want nil", got)
	}
}

func TestPreopensHost_FromWASICallConfig(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewPreopensHost(resources)

	dataFS := fsapi.NewReadOnlyFS(fstest.MapFS{
		"file.txt": {Data: []byte("data")},
	})
	tmpFS := fsapi.NewReadOnlyFS(fstest.MapFS{})

	ctx := wippyhost.WithWASICallConfig(context.Background(), &wippyhost.WASICallConfig{
		Mounts: []wippyhost.WASIMountBinding{
			{
				Filesystem: dataFS,
				Guest:      "/data",
				ReadOnly:   true,
			},
			{
				Filesystem: tmpFS,
				Guest:      "/tmp",
				ReadOnly:   false,
			},
		},
	})

	dirs := host.GetDirectories(ctx)
	if len(dirs) != 2 {
		t.Fatalf("GetDirectories() len = %d, want 2", len(dirs))
	}

	guest0, ok := dirs[0][1].(string)
	if !ok || guest0 != "/data" {
		t.Fatalf("dirs[0][1] = %#v, want /data", dirs[0][1])
	}
	h0, ok := dirs[0][0].(uint32)
	if !ok || h0 == 0 {
		t.Fatalf("dirs[0][0] = %#v, want non-zero handle", dirs[0][0])
	}
	r0, ok := resources.Get(h0)
	if !ok {
		t.Fatalf("resource for handle %d not found", h0)
	}
	d0, ok := r0.(*descriptorResource)
	if !ok {
		t.Fatalf("resource type = %T, want *descriptorResource", r0)
	}
	if d0.fs != dataFS || !d0.readOnly {
		t.Fatalf("descriptor = fs:%v readOnly:%v", d0.fs != nil, d0.readOnly)
	}

	guest1, ok := dirs[1][1].(string)
	if !ok || guest1 != "/tmp" {
		t.Fatalf("dirs[1][1] = %#v, want /tmp", dirs[1][1])
	}
}
