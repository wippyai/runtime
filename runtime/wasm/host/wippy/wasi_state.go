// SPDX-License-Identifier: MPL-2.0

package wippy

import (
	"context"

	fsapi "github.com/wippyai/runtime/api/fs"
)

type wasiCallConfigKey struct{}

// WASIMountBinding is a resolved mount mapping for a single WASM invocation.
type WASIMountBinding struct {
	Filesystem fsapi.FS
	Guest      string
	// Host is the absolute host directory backing Filesystem, when it is
	// host-backed. Preferred for mounting (wazero sandboxes the guest to exactly
	// this directory and it supports writes); empty for non-directory
	// filesystems, which are mounted read-only through Filesystem.
	Host     string
	ReadOnly bool
}

// WASICallConfig carries resolved per-invocation WASI settings.
// Hosts read this from context so host registration stays global/once.
type WASICallConfig struct {
	Args   []string
	Cwd    string
	Env    map[string]string
	Mounts []WASIMountBinding
}

// WithWASICallConfig stores resolved invocation-specific WASI settings in context.
func WithWASICallConfig(ctx context.Context, cfg *WASICallConfig) context.Context {
	if cfg == nil {
		return ctx
	}
	return context.WithValue(ctx, wasiCallConfigKey{}, cfg)
}

// GetWASICallConfig returns invocation-specific WASI settings from context.
func GetWASICallConfig(ctx context.Context) *WASICallConfig {
	if ctx == nil {
		return nil
	}
	cfg, _ := ctx.Value(wasiCallConfigKey{}).(*WASICallConfig)
	return cfg
}
