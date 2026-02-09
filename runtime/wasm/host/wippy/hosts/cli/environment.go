package cli

import (
	"context"
	"sort"

	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
)

const (
	// EnvironmentNamespace exposes WASI preview2 CLI environment APIs.
	EnvironmentNamespace = "wasi:cli/environment@0.2.3"
)

// EnvironmentHost provides invocation-scoped WASI env/args/cwd data.
type EnvironmentHost struct{}

// NewEnvironmentHost builds a WASI CLI environment host.
func NewEnvironmentHost() *EnvironmentHost {
	return &EnvironmentHost{}
}

// Namespace implements wasm-runtime Host.
func (h *EnvironmentHost) Namespace() string {
	return EnvironmentNamespace
}

// GetEnvironment returns environment entries mapped from wasi.env[].
func (h *EnvironmentHost) GetEnvironment(ctx context.Context) [][2]string {
	cfg := wippyhost.GetWASICallConfig(ctx)
	if cfg == nil || len(cfg.Env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(cfg.Env))
	for key := range cfg.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([][2]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, [2]string{key, cfg.Env[key]})
	}
	return out
}

// GetArguments returns invocation args mapped from wasi.args[].
func (h *EnvironmentHost) GetArguments(ctx context.Context) []string {
	cfg := wippyhost.GetWASICallConfig(ctx)
	if cfg != nil && len(cfg.Args) > 0 {
		out := make([]string, len(cfg.Args))
		copy(out, cfg.Args)
		return out
	}

	tc := terminalapi.GetTerminalContext(ctx)
	if tc == nil || len(tc.Args) == 0 {
		return nil
	}
	out := make([]string, len(tc.Args))
	copy(out, tc.Args)
	return out
}

// InitialCwd returns invocation cwd mapped from wasi.cwd.
func (h *EnvironmentHost) InitialCwd(ctx context.Context) *string {
	cfg := wippyhost.GetWASICallConfig(ctx)
	if cfg == nil || cfg.Cwd == "" {
		cwd := "/"
		return &cwd
	}
	cwd := cfg.Cwd
	return &cwd
}
