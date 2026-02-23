// SPDX-License-Identifier: MPL-2.0

//go:build windows

package extensions

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/boot"
)

// Load is a no-op on Windows since Go extensions are unsupported.
func Load(ctx context.Context, cfg boot.Config) (context.Context, Result, error) {
	if cfg == nil {
		return ctx, Result{}, nil
	}
	sub := cfg.Sub("extensions")
	if !sub.GetBool("enabled", true) {
		return ctx, Result{}, nil
	}
	if !hasPaths(sub) {
		return ctx, Result{}, nil
	}
	return ctx, Result{}, fmt.Errorf("extensions are not supported on Windows")
}

// LoadWithReserved mirrors Load on Windows (extensions unsupported).
func LoadWithReserved(ctx context.Context, cfg boot.Config, _ map[string]struct{}) (context.Context, Result, error) {
	return Load(ctx, cfg)
}

func hasPaths(cfg boot.Config) bool {
	val, ok := cfg.Get("paths")
	if !ok || val == nil {
		return false
	}
	switch v := val.(type) {
	case []string:
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				return true
			}
		}
		return false
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
		}
		return false
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return true
	}
}
