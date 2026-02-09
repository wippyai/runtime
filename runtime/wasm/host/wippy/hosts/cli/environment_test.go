package cli

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
)

func TestEnvironmentHost_Empty(t *testing.T) {
	host := NewEnvironmentHost()
	if host.Namespace() != EnvironmentNamespace {
		t.Fatalf("Namespace() = %q, want %q", host.Namespace(), EnvironmentNamespace)
	}

	if got := host.GetEnvironment(context.Background()); got != nil {
		t.Fatalf("GetEnvironment() = %#v, want nil", got)
	}
	if got := host.GetArguments(context.Background()); got != nil {
		t.Fatalf("GetArguments() = %#v, want nil", got)
	}

	cwd := host.InitialCwd(context.Background())
	if cwd == nil || *cwd != "/" {
		t.Fatalf("InitialCwd() = %v, want /", cwd)
	}
}

func TestEnvironmentHost_FromWASICallConfig(t *testing.T) {
	ctx := wippyhost.WithWASICallConfig(context.Background(), &wippyhost.WASICallConfig{
		Args: []string{"--a", "--b"},
		Cwd:  "/work",
		Env: map[string]string{
			"API_KEY": "secret",
			"MODE":    "test",
		},
	})

	host := NewEnvironmentHost()

	env := host.GetEnvironment(ctx)
	if len(env) != 2 {
		t.Fatalf("GetEnvironment() len = %d, want 2", len(env))
	}
	if env[0][0] != "API_KEY" || env[0][1] != "secret" {
		t.Fatalf("GetEnvironment()[0] = %#v", env[0])
	}
	if env[1][0] != "MODE" || env[1][1] != "test" {
		t.Fatalf("GetEnvironment()[1] = %#v", env[1])
	}

	args := host.GetArguments(ctx)
	if len(args) != 2 || args[0] != "--a" || args[1] != "--b" {
		t.Fatalf("GetArguments() = %#v, want [\"--a\",\"--b\"]", args)
	}

	cwd := host.InitialCwd(ctx)
	if cwd == nil || *cwd != "/work" {
		t.Fatalf("InitialCwd() = %v, want /work", cwd)
	}
}

func TestEnvironmentHost_ArgumentsFromTerminalContext(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer func() { _ = fc.Close() }()

	tc := terminalapi.NewTerminalContextWithArgs(nil, nil, nil, []string{"--tty", "--debug"})
	if err := terminalapi.WithTerminalContext(ctx, tc); err != nil {
		t.Fatalf("WithTerminalContext() error = %v", err)
	}

	host := NewEnvironmentHost()
	args := host.GetArguments(ctx)
	if len(args) != 2 || args[0] != "--tty" || args[1] != "--debug" {
		t.Fatalf("GetArguments() = %#v, want [\"--tty\",\"--debug\"]", args)
	}
}
