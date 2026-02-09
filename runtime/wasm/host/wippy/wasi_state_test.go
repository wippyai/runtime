package wippy

import (
	"context"
	"testing"
	"testing/fstest"

	fsapi "github.com/wippyai/runtime/api/fs"
)

func TestWASICallConfigContext(t *testing.T) {
	base := context.Background()
	if cfg := GetWASICallConfig(base); cfg != nil {
		t.Fatalf("GetWASICallConfig() = %#v, want nil", cfg)
	}

	mockFS := fsapi.NewReadOnlyFS(fstest.MapFS{})
	want := &WASICallConfig{
		Args: []string{"--x"},
		Cwd:  "/work",
		Env: map[string]string{
			"API_KEY": "secret",
		},
		Mounts: []WASIMountBinding{
			{
				Filesystem: mockFS,
				Guest:      "/data",
				ReadOnly:   true,
			},
		},
	}
	ctx := WithWASICallConfig(base, want)
	got := GetWASICallConfig(ctx)
	if got == nil {
		t.Fatal("GetWASICallConfig() returned nil")
	}
	if got.Cwd != want.Cwd {
		t.Fatalf("Cwd = %q, want %q", got.Cwd, want.Cwd)
	}
	if len(got.Args) != 1 || got.Args[0] != "--x" {
		t.Fatalf("Args = %#v, want [\"--x\"]", got.Args)
	}
	if got.Env["API_KEY"] != "secret" {
		t.Fatalf("Env[API_KEY] = %q, want %q", got.Env["API_KEY"], "secret")
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Guest != "/data" {
		t.Fatalf("Mounts = %#v", got.Mounts)
	}
	if got.Mounts[0].Filesystem == nil {
		t.Fatal("Mounts[0].Filesystem is nil")
	}
	if !got.Mounts[0].ReadOnly {
		t.Fatal("Mounts[0].ReadOnly = false, want true")
	}
}
