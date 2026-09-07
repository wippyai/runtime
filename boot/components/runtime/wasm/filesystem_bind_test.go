// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	_ "embed"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	fsapi "github.com/wippyai/runtime/api/fs"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"

	"github.com/wippyai/runtime/api/registry"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

//go:embed testdata/filesystem_preview1_probe.wasm
var filesystemPreview1Probe []byte

func TestWASIPreview1AdaptedComponentReadsMount(t *testing.T) {
	ctx := wippyhost.WithWASICallConfig(context.Background(), &wippyhost.WASICallConfig{Mounts: []wippyhost.WASIMountBinding{{Guest: "/data", Filesystem: fsapi.NewReadOnlyFS(fstest.MapFS{"input.txt": {Data: []byte("mount-ok")}}), ReadOnly: true}}})
	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer rt.Close(ctx)

	hosts := wasmcomponent.NewHostRegistry()
	if err := hosts.RegisterProfiles(DefaultHostProfiles(zap.NewNop(), nil)...); err != nil {
		t.Fatalf("register profiles: %v", err)
	}
	imports := []registry.ID{
		registry.ParseID("wasi:io"),
		registry.ParseID("wasi:cli"),
		registry.ParseID("wasi:clocks"),
		registry.ParseID("wasi:filesystem"),
		registry.ParseID("wasi:random"),
	}
	if err := hosts.EnsureImports(ctx, rt, imports, true); err != nil {
		t.Fatalf("register imported hosts: %v", err)
	}

	module, err := rt.LoadComponent(ctx, filesystemPreview1Probe)
	if err != nil {
		t.Fatalf("load filesystem component: %v", err)
	}
	if err := module.Compile(ctx); err != nil {
		t.Fatalf("bind filesystem component: %v", err)
	}

	inst, err := module.Instantiate(ctx)
	require.NoError(t, err)
	defer inst.Close(ctx)
	result, err := inst.Call(ctx, "check")
	require.NoError(t, err)
	require.Equal(t, uint32(0), result, "adapted libc must open and read the mounted file")
}
