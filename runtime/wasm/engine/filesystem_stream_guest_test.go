// SPDX-License-Identifier: MPL-2.0
package engine

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	secapi "github.com/wippyai/runtime/api/security"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	filesystemhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/filesystem"
	iohost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/io"
	pollhost "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/poll"
	"github.com/wippyai/runtime/service/fs/directory"
	socketservice "github.com/wippyai/runtime/service/socket"
	secsys "github.com/wippyai/runtime/system/security"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type filesystemGuestPolicy struct{}

func (filesystemGuestPolicy) ID() registry.ID { return registry.ParseID("test:filesystem-policy") }
func (filesystemGuestPolicy) Evaluate(principal secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	if principal.ID == "filesystem-guest" && action == "fs.get" && resource == "test:files" {
		return secapi.Allow
	}
	return secapi.Deny
}

type gatedDescriptorFS struct {
	*directory.FS
	release <-chan struct{}
}

func (f *gatedDescriptorFS) OpenDescriptorAt(parent fs.File, name string, req fsapi.DescriptorOpenRequest) (fsapi.File, error) {
	file, err := f.FS.OpenDescriptorAt(parent, name, req)
	if err != nil || name != "input" {
		return file, err
	}
	return &gatedDescriptorFile{File: file, release: f.release}, nil
}

type gatedDescriptorFile struct {
	fsapi.File
	release <-chan struct{}
}

func (f *gatedDescriptorFile) ReadAt(buf []byte, offset int64) (int, error) {
	<-f.release
	if r, ok := f.File.(io.ReaderAt); ok {
		return r.ReadAt(buf, offset)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return f.Read(buf)
}

// Exercises real canonical filesystem resources and Asyncify stream suspension.
// The guest drops each descriptor before using its derived stream, so the
// retained file lease and dispatcher-owned wait are both necessary for success.
func TestFilesystemGuestBoundedStreamsAndDescriptorLifetime(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer frame.Close()
	require.NoError(t, secapi.SetActor(ctx, secapi.Actor{ID: "filesystem-guest"}))
	require.NoError(t, secapi.SetScope(ctx, secsys.NewScope([]secapi.Policy{filesystemGuestPolicy{}})))
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	require.NoError(t, err)
	defer rt.Close(ctx)
	buffers := preview2.NewHostBufferBudget(2 * 65536)
	table := preview2.NewResourceTableWithBudgets(32, preview2.NewSocketBudget(1), buffers)
	defer table.Close()
	for _, host := range []wasmrt.Host{filesystemhost.NewTypesHost(table), filesystemhost.NewPreopensHost(table), iohost.NewStreamsHost(table), iohost.NewErrorHost(table), pollhost.NewHost(table)} {
		require.NoError(t, rt.RegisterHost(host))
	}
	code, err := os.ReadFile("testdata/filesystem_stream.wasm")
	require.NoError(t, err)
	module, err := rt.LoadComponent(ctx, code)
	require.NoError(t, err)
	require.NoError(t, module.Compile(ctx))
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "input"), bytes.Repeat([]byte{7}, 4<<20), 0600))
	filesystem, err := directory.NewFS(root, 0700, false)
	require.NoError(t, err)
	defer filesystem.Close()
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	mounted := &gatedDescriptorFS{FS: filesystem, release: release}
	wasi := wasmapi.WASIConfig{Mounts: []wasmapi.WASIMountConfig{{FS: registry.ParseID("test:files"), Guest: "/data"}}}
	p := NewActorProcess(NewProcess(module, "", wasi, wasmapi.LimitsConfig{MaxExecutionMS: 10000}, &testFSRegistry{entries: map[string]fsapi.FS{"test:files": mounted}}), actor.DefaultLimits(), nil)
	defer p.Close()
	require.NoError(t, p.Init(ctx, "run", nil))
	handlers := make(map[dispatcher.CommandID]dispatcher.Handler)
	socketservice.NewDispatcher(nil).RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { handlers[id] = h })
	waitCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var events []process.Event
	var out process.StepOutput
	waits := 0
	for steps := 0; steps < 1024; steps++ {
		out.Reset()
		require.NoError(t, p.Step(events, &out))
		if out.IsDone() {
			require.Equal(t, map[string]any{"ok": "4194301:29360107"}, out.Result().Data())
			require.Positive(t, waits, "blocked file read never suspended")
			data, err := os.ReadFile(filepath.Join(root, "output"))
			require.NoError(t, err)
			require.Equal(t, "4194301:29360107", string(data))
			require.LessOrEqual(t, buffers.Usage().Peak, uint64(2*65536))
			require.Eventually(t, func() bool { return buffers.Usage().Used == 0 }, time.Second, time.Millisecond, "file workers retained buffer charges after guest drop")
			return
		}
		require.Equal(t, 1, out.Count())
		y := out.Yields()[0]
		_, ok := y.Cmd.(*socketapi.StreamWaitCmd)
		require.True(t, ok, "filesystem read escaped the stream-wait protocol: %T", y.Cmd)
		waits++
		once.Do(func() { close(release) })
		handler := handlers[y.Cmd.CmdID()]
		require.NotNil(t, handler)
		receiver := &mqttGuestReceiver{done: make(chan process.Event, 1)}
		require.NoError(t, handler.Handle(waitCtx, y.Cmd, y.Tag, receiver))
		select {
		case event := <-receiver.done:
			require.NoError(t, receiver.err)
			events = []process.Event{event}
		case <-waitCtx.Done():
			t.Fatal("filesystem stream wait timed out")
		}
	}
	t.Fatal("filesystem guest exceeded step limit")
}
