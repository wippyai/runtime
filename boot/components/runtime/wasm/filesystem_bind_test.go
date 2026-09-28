// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	_ "embed"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	socketapi "github.com/wippyai/runtime/api/socket"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	"github.com/wippyai/runtime/service/fs/directory"
	socketservice "github.com/wippyai/runtime/service/socket"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

//go:embed testdata/filesystem_preview1_probe.wasm
var filesystemPreview1Probe []byte

func TestWASIPreview1AdaptedComponentReadsMount(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "input.txt"), []byte("mount-ok"), 0600))
	filesystem, err := directory.NewFS(root, 0500, false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, filesystem.Close()) })
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	checkAdaptedFilesystemMount(t, &gatedProbeFS{FS: filesystem, release: release}, 0, unblock)
}

func TestWASIPreview1AdaptedComponentRejectsUnretainedProvider(t *testing.T) {
	// A generic ReadDirFS has no handle-relative child-open capability. The
	// adapter must expose unsupported, never silently reopen its path.
	filesystem := fsapi.NewReadOnlyFS(fstest.MapFS{"input.txt": {Data: []byte("mount-ok")}})
	checkAdaptedFilesystemMount(t, filesystem, 58, nil) // WASI ENOTSUP
}

func checkAdaptedFilesystemMount(t *testing.T, filesystem fsapi.FS, expected uint32, unblock func()) {
	t.Helper()
	ctx, cancelExecution := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelExecution()
	ctx = wippyhost.WithWASICallConfig(ctx, &wippyhost.WASICallConfig{Mounts: []wippyhost.WASIMountBinding{{Guest: "/data", Filesystem: filesystem, ReadOnly: true}}})
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

	// The official preview1 adapter reads through preview2 blocking streams.
	// Drive the runtime suspension protocol rather than requiring synchronous I/O.
	proc := wasmengine.NewProcess(module, "", wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer proc.Close()
	// Release a gated read before process teardown even if Step fails before
	// returning its first yield. t.Cleanup runs after these deferred closes.
	if unblock != nil {
		defer unblock()
	}
	require.NoError(t, proc.Init(ctx, "check", nil))
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	socketservice.NewDispatcher(nil).RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var events []process.Event
	var out process.StepOutput
	waits := 0
	for step := range 32 {
		out.Reset()
		require.NoError(t, proc.Step(events, &out), "step %d with %d completion events", step, len(events))
		if out.IsDone() {
			if unblock != nil {
				require.Positive(t, waits, "adapted read must exercise cross-core suspension")
			}
			require.NotNil(t, out.Result())
			require.Equal(t, expected, out.Result().Data(), "adapted libc must respect the mounted provider capability")
			return
		}
		require.Equal(t, 1, out.Count())
		y := out.Yields()[0]
		require.IsType(t, &socketapi.StreamWaitCmd{}, y.Cmd)
		waits++
		if unblock != nil {
			unblock()
		}
		handler := handlers[y.Cmd.CmdID()]
		require.NotNil(t, handler)
		receiver := &filesystemProbeReceiver{events: make(chan process.Event, 1)}
		require.NoError(t, handler.Handle(waitCtx, y.Cmd, y.Tag, receiver))
		select {
		case event := <-receiver.events:
			require.NoError(t, event.Error)
			events = []process.Event{event}
		case <-waitCtx.Done():
			t.Fatal("adapted filesystem stream wait timed out")
		}
	}
	t.Fatal("adapted filesystem probe exceeded step bound")
}

type filesystemProbeReceiver struct{ events chan process.Event }

func (r *filesystemProbeReceiver) CompleteYield(tag uint64, data any, err error) {
	r.events <- process.Event{Type: process.EventYieldComplete, Tag: tag, Data: data, Error: err}
}

// Hold the first host read until a real dispatcher yield: otherwise a warm local
// file may be ready immediately and accidentally bypass the suspension contract.
type gatedProbeFS struct {
	*directory.FS
	release <-chan struct{}
}

func (f *gatedProbeFS) OpenDescriptorAt(parent fs.File, name string, request fsapi.DescriptorOpenRequest) (fsapi.File, error) {
	file, err := f.FS.OpenDescriptorAt(parent, name, request)
	if err != nil || name != "input.txt" {
		return file, err
	}
	return &gatedProbeFile{File: file, release: f.release}, nil
}

type gatedProbeFile struct {
	fsapi.File
	release <-chan struct{}
}

func (f *gatedProbeFile) ReadAt(buf []byte, offset int64) (int, error) {
	<-f.release
	if reader, ok := f.File.(io.ReaderAt); ok {
		return reader.ReadAt(buf, offset)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return f.Read(buf)
}
