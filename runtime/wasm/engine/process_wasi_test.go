// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/service/fs/directory"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

type testEnvRegistry struct {
	values map[string]string
}

func (r *testEnvRegistry) Get(ctx context.Context, name string) (string, error) {
	value, found, err := r.Lookup(ctx, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", envapi.ErrVariableNotFound
	}
	return value, nil
}

func (r *testEnvRegistry) Lookup(_ context.Context, name string) (string, bool, error) {
	if r.values == nil {
		return "", false, envapi.ErrVariableNotFound
	}
	value, found := r.values[name]
	if !found || value == "" {
		return "", false, envapi.ErrVariableNotFound
	}
	return value, true, nil
}

func (r *testEnvRegistry) Set(context.Context, string, string) error {
	return errors.New("not implemented")
}

func (r *testEnvRegistry) All(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *testEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}

func (r *testEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) {}
func (r *testEnvRegistry) RegisterVariable(envapi.Variable) error      { return nil }
func (r *testEnvRegistry) UnregisterVariable(registry.ID)              {}

type testFSRegistry struct {
	entries map[string]fsapi.FS
}

func (r *testFSRegistry) GetFS(name string) (fsapi.FS, bool) {
	if r.entries == nil {
		return nil, false
	}
	fs, ok := r.entries[name]
	return fs, ok
}

type testHostPathFS struct {
	fsapi.FS
	root string
}

func (f *testHostPathFS) RootPath() string { return f.root }

func TestResolveWASICallConfig_Empty(t *testing.T) {
	p := &Process{}
	cfg, err := p.resolveWASICallConfig(ctxapi.NewRootContext())
	if err != nil {
		t.Fatalf("resolveWASICallConfig() error = %v", err)
	}
	if cfg != nil {
		t.Fatalf("resolveWASICallConfig() = %#v, want nil", cfg)
	}
}

func TestResolveWASICallConfig_ResolvesEnvAndMounts(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	ctx = envapi.WithRegistry(ctx, &testEnvRegistry{
		values: map[string]string{
			"app.env:api_key": "secret",
		},
	})

	mockFS := fsapi.NewReadOnlyFS(fstest.MapFS{})
	p := &Process{
		wasi: wasmapi.WASIConfig{
			Cwd:  "/work",
			Args: []string{"--fast"},
			Env: []wasmapi.WASIEnvVarConfig{
				{
					ID:       registry.ParseID("app.env:api_key"),
					Name:     "API_KEY",
					Required: true,
				},
			},
			Mounts: []wasmapi.WASIMountConfig{
				{
					FS:       registry.ParseID("app.fs:data"),
					Guest:    "/data",
					ReadOnly: true,
				},
			},
		},
		fsReg: &testFSRegistry{
			entries: map[string]fsapi.FS{
				"app.fs:data": mockFS,
			},
		},
	}

	cfg, err := p.resolveWASICallConfig(ctx)
	if err != nil {
		t.Fatalf("resolveWASICallConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("resolveWASICallConfig() returned nil config")
		return
	}
	if cfg.Cwd != "/work" {
		t.Fatalf("cfg.Cwd = %q, want %q", cfg.Cwd, "/work")
	}
	if len(cfg.Args) != 1 || cfg.Args[0] != "--fast" {
		t.Fatalf("cfg.Args = %#v, want [\"--fast\"]", cfg.Args)
	}
	if got := cfg.Env["API_KEY"]; got != "secret" {
		t.Fatalf("cfg.Env[API_KEY] = %q, want %q", got, "secret")
	}
	if len(cfg.Mounts) != 1 {
		t.Fatalf("cfg.Mounts len = %d, want 1", len(cfg.Mounts))
	}
	if cfg.Mounts[0].Guest != "/data" || !cfg.Mounts[0].ReadOnly {
		t.Fatalf("cfg.Mounts[0] = %#v", cfg.Mounts[0])
	}
	if cfg.Mounts[0].Filesystem != mockFS {
		t.Fatal("cfg.Mounts[0].Filesystem does not match expected FS instance")
	}
}

func TestResolveWASICallConfig_ReadOnlyFilesystemRetainsCapability(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	base := &testHostPathFS{
		FS:   fsapi.NewReadOnlyFS(fstest.MapFS{}),
		root: t.TempDir(),
	}
	readOnly := fsapi.NewReadOnlyFS(base)
	p := &Process{
		wasi: wasmapi.WASIConfig{Mounts: []wasmapi.WASIMountConfig{{
			FS:       registry.ParseID("app.fs:data"),
			Guest:    "/data",
			ReadOnly: false,
		}}},
		fsReg: &testFSRegistry{entries: map[string]fsapi.FS{"app.fs:data": readOnly}},
	}

	cfg, err := p.resolveWASICallConfig(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Mounts, 1)
	assert.Same(t, readOnly, cfg.Mounts[0].Filesystem)
}

func TestResolveWASICallConfig_WritableFilesystemRetainsCapability(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	hostPath := t.TempDir()
	writable := &testHostPathFS{
		FS:   fsapi.NewReadOnlyFS(fstest.MapFS{}),
		root: hostPath,
	}
	p := &Process{
		wasi: wasmapi.WASIConfig{Mounts: []wasmapi.WASIMountConfig{{
			FS:    registry.ParseID("app.fs:data"),
			Guest: "/data",
		}}},
		fsReg: &testFSRegistry{entries: map[string]fsapi.FS{"app.fs:data": writable}},
	}

	cfg, err := p.resolveWASICallConfig(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Mounts, 1)
	assert.Same(t, writable, cfg.Mounts[0].Filesystem)
}

func TestResolveWASICallConfig_RequiredEnvMissing(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	ctx = envapi.WithRegistry(ctx, &testEnvRegistry{values: map[string]string{}})

	p := &Process{
		wasi: wasmapi.WASIConfig{
			Env: []wasmapi.WASIEnvVarConfig{
				{
					ID:       registry.ParseID("app.env:missing"),
					Name:     "MISSING",
					Required: true,
				},
			},
		},
	}

	_, err := p.resolveWASICallConfig(ctx)
	if err == nil {
		t.Fatal("resolveWASICallConfig() expected error for required missing env")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "required wasi env variable not found") {
		t.Fatalf("error = %q, want required wasi env variable not found", got)
	}
}

func TestResolveWASICallConfig_OptionalEnvMissing(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	ctx = envapi.WithRegistry(ctx, &testEnvRegistry{values: map[string]string{}})

	p := &Process{
		wasi: wasmapi.WASIConfig{
			Env: []wasmapi.WASIEnvVarConfig{
				{
					ID:   registry.ParseID("app.env:missing"),
					Name: "MISSING",
				},
			},
		},
	}

	cfg, err := p.resolveWASICallConfig(ctx)
	if err != nil {
		t.Fatalf("resolveWASICallConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("resolveWASICallConfig() returned nil config")
		return
	}
	if len(cfg.Env) != 0 {
		t.Fatalf("cfg.Env = %#v, want empty", cfg.Env)
	}
}

func TestResolveWASICallConfig_MountFSMissing(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)

	p := &Process{
		wasi: wasmapi.WASIConfig{
			Mounts: []wasmapi.WASIMountConfig{
				{
					FS:    registry.ParseID("app.fs:missing"),
					Guest: "/data",
				},
			},
		},
		fsReg: &testFSRegistry{
			entries: map[string]fsapi.FS{},
		},
	}

	_, err := p.resolveWASICallConfig(ctx)
	if err == nil {
		t.Fatal("resolveWASICallConfig() expected error for missing mount fs")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "wasi mount filesystem not found") {
		t.Fatalf("error = %q, want wasi mount filesystem not found", got)
	}
}

// TestProcess_WASIRegisteredMountConfinement uses the production Process mount
// resolution with an os.Root-backed registered filesystem and a real WASI guest.
func TestProcess_WASIRegisteredMountConfinement(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		for _, escape := range []bool{false, true} {
			t.Run(fmt.Sprintf("readonly=%v/escape=%v", readOnly, escape), func(t *testing.T) {
				ctx := ctxapi.NewRootContext()
				secapi.SetStrictMode(ctx, false)
				root := t.TempDir()
				target := filepath.Join(root, "escape")
				if escape {
					target = t.TempDir()
				} else if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "sentinel"), []byte("OWNED-TEST-SENTINEL-OUTSIDE-GRANT"), 0600); err != nil {
					t.Fatal(err)
				}
				if escape {
					if err := os.Symlink(target, filepath.Join(root, "escape")); err != nil {
						t.Fatal(err)
					}
				}
				registered, err := directory.NewFS(root, 0700, false)
				if err != nil {
					t.Fatal(err)
				}
				defer registered.Close()
				rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
				if err != nil {
					t.Fatal(err)
				}
				defer rt.Close(ctx)
				source, err := os.ReadFile("testdata/internal-validation/mount-probe.wat")
				if err != nil {
					t.Fatal(err)
				}
				mod, err := rt.LoadWAT(ctx, string(source), "probe: func() -> u32;")
				if err != nil {
					t.Fatal(err)
				}
				if err := mod.Compile(ctx); err != nil {
					t.Fatal(err)
				}
				p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{Mounts: []wasmapi.WASIMountConfig{{FS: registry.ParseID("test:root"), Guest: "/mount", ReadOnly: readOnly}}}, wasmapi.LimitsConfig{}, &testFSRegistry{entries: map[string]fsapi.FS{"test:root": registered}})
				defer p.Close()
				if err := p.Init(ctx, "probe", nil); err != nil {
					t.Fatal(err)
				}
				var out process.StepOutput
				if err := p.Step(nil, &out); err != nil {
					t.Fatal(err)
				}
				if !out.IsDone() || out.Result() == nil {
					t.Fatal("missing guest result")
				}
				errno, ok := out.Result().Data().(uint32)
				if !ok {
					t.Fatalf("unexpected errno result type %T", out.Result().Data())
				}
				if escape && errno == 0 {
					t.Fatal("guest opened a host symlink outside its registered filesystem")
				}
				if !escape && errno != 0 {
					t.Fatalf("ordinary confined read failed: errno=%d", errno)
				}
			})
		}
	}
}

// capabilityTrackedFS borrows the registered filesystem and tracks only files
// it lends to WASI. The registry itself must remain usable after Process.Close.
type capabilityTrackedFS struct {
	fsapi.FS
	opened, closed atomic.Int64
}

func (f *capabilityTrackedFS) RootPath() string { return f.FS.(fsapi.HostPathFS).RootPath() }

type capabilityTrackedReadFile struct {
	fs.File
	owner *capabilityTrackedFS
}

func (f *capabilityTrackedReadFile) Close() error { f.owner.closed.Add(1); return f.File.Close() }

type capabilityTrackedWriteFile struct {
	fsapi.File
	owner *capabilityTrackedFS
}

func (f *capabilityTrackedWriteFile) Close() error { f.owner.closed.Add(1); return f.File.Close() }
func (f *capabilityTrackedFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	f.opened.Add(1)
	return &capabilityTrackedReadFile{file, f}, nil
}
func (f *capabilityTrackedFS) OpenFile(name string, flags int, mode fs.FileMode) (fsapi.File, error) {
	file, err := f.FS.OpenFile(name, flags, mode)
	if err != nil {
		return nil, err
	}
	f.opened.Add(1)
	return &capabilityTrackedWriteFile{file, f}, nil
}
func TestProcess_WASIRegisteredMountWritePermissionsAndClose(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     fs.FileMode
		readonly bool
		allow    bool
		escape   bool
	}{
		{name: "writable", readonly: false, mode: 0700, allow: true, escape: false}, {name: "readonly-mount", readonly: true, mode: 0700, allow: false, escape: false}, {name: "readonly-registry", readonly: false, mode: 0500, allow: false, escape: false}, {name: "symlink-write", readonly: false, mode: 0700, allow: false, escape: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ctxapi.NewRootContext()
			secapi.SetStrictMode(ctx, false)
			root := t.TempDir()
			path := filepath.Join(root, "file.txt")
			const original = "ORIGINAL-MUST-NOT-BE-TRUNCATED"
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.escape {
				outside := filepath.Join(t.TempDir(), "sentinel")
				if err := os.WriteFile(outside, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			registered, err := directory.NewFS(root, tc.mode, false)
			if err != nil {
				t.Fatal(err)
			}
			defer registered.Close()
			tracked := &capabilityTrackedFS{FS: registered}
			rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
			if err != nil {
				t.Fatal(err)
			}
			defer rt.Close(ctx)
			source, err := os.ReadFile("testdata/internal-validation/mount-write.wat")
			if err != nil {
				t.Fatal(err)
			}
			mod, err := rt.LoadWAT(ctx, string(source), "probe: func() -> u32;")
			if err != nil {
				t.Fatal(err)
			}
			if err := mod.Compile(ctx); err != nil {
				t.Fatal(err)
			}
			p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{Mounts: []wasmapi.WASIMountConfig{{FS: registry.ParseID("test:root"), Guest: "/mount", ReadOnly: tc.readonly}}}, wasmapi.LimitsConfig{}, &testFSRegistry{entries: map[string]fsapi.FS{"test:root": tracked}})
			defer p.Close()
			if err := p.Init(ctx, "probe", nil); err != nil {
				t.Fatal(err)
			}
			var out process.StepOutput
			if err := p.Step(nil, &out); err != nil {
				t.Fatal(err)
			}
			if !out.IsDone() || out.Result() == nil {
				t.Fatal("missing guest result")
			}
			errno, ok := out.Result().Data().(uint32)
			if !ok {
				t.Fatalf("unexpected errno type %T", out.Result().Data())
			}
			if tc.allow && errno != 0 {
				t.Fatalf("writable mount failed: errno=%d", errno)
			}
			if !tc.allow && errno == 0 {
				t.Fatal("guest bypassed declared write restriction")
			}
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := original
			if tc.allow {
				want = "GUESTOK"
			}
			if string(actual) != want {
				t.Fatalf("file content=%q, want %q", actual, want)
			}
			// Guest intentionally leaves its file descriptor open.
			p.Close()
			if tc.allow && tracked.opened.Load() == 0 {
				t.Fatal("filesystem capability was bypassed")
			}
			if tracked.opened.Load() != tracked.closed.Load() {
				t.Fatalf("file ownership mismatch: opened=%d closed=%d", tracked.opened.Load(), tracked.closed.Load())
			}
			p.Close()
			if tracked.opened.Load() != tracked.closed.Load() {
				t.Fatal("repeated Close closed a file twice")
			}
			file, err := registered.Open(".")
			if err != nil {
				t.Fatalf("Process closed borrowed registry filesystem: %v", err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestProcess_WASIRegisteredMountMissingFileErrno protects the optional-file
// lookup used during Python initialization. Nested root/service errors must
// remain WASI ENOENT (44), rather than becoming generic EIO (29).
func TestProcess_WASIRegisteredMountMissingFileErrno(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	secapi.SetStrictMode(ctx, false)
	registered, err := directory.NewFS(t.TempDir(), 0700, false)
	if err != nil {
		t.Fatal(err)
	}
	defer registered.Close()
	rt, err := wasmrt.NewWithConfig(ctx, &wasmrt.Config{CloseOnContextDone: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	source, err := os.ReadFile("testdata/internal-validation/mount-probe.wat")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.LoadWAT(ctx, string(source), "probe: func() -> u32;")
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Compile(ctx); err != nil {
		t.Fatal(err)
	}
	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{Mounts: []wasmapi.WASIMountConfig{{FS: registry.ParseID("test:root"), Guest: "/mount", ReadOnly: true}}}, wasmapi.LimitsConfig{}, &testFSRegistry{entries: map[string]fsapi.FS{"test:root": registered}})
	defer p.Close()
	if err := p.Init(ctx, "probe", nil); err != nil {
		t.Fatal(err)
	}
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsDone() || out.Result() == nil {
		t.Fatal("missing guest result")
	}
	errno, ok := out.Result().Data().(uint32)
	if !ok {
		t.Fatalf("unexpected errno type %T", out.Result().Data())
	}
	if errno != 44 {
		t.Fatalf("missing optional file: WASI errno=%d, want ENOENT(44)", errno)
	}
}
