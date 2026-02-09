package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	secapi "github.com/wippyai/runtime/api/security"
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

func TestResolveWASICallConfig_RequiredEnvMissing(t *testing.T) {
	ctx := ctxapi.NewRootContext()
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
