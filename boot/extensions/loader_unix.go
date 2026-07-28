// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package extensions

import (
	"context"
	"fmt"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	extensionapi "github.com/wippyai/runtime/api/extension"
	logapi "github.com/wippyai/runtime/api/logs"
	"go.uber.org/zap"
)

// Load loads extensions from config and returns their components.
func Load(ctx context.Context, cfg boot.Config) (context.Context, Result, error) {
	return LoadWithReserved(ctx, cfg, nil)
}

// LoadWithReserved loads extensions and rejects components that collide with reserved names.
func LoadWithReserved(ctx context.Context, cfg boot.Config, reserved map[string]struct{}) (context.Context, Result, error) {
	result := Result{}
	logger := logapi.GetLogger(ctx)
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("extensions")
	if cfg == nil {
		return ctx, result, nil
	}

	sub := cfg.Sub("extensions")
	if !sub.GetBool("enabled", true) {
		logger.Debug("extensions disabled")
		return ctx, result, nil
	}

	paths, err := parsePaths(sub)
	if err != nil {
		logger.Error("invalid extension config", zap.Error(err))
		return ctx, result, newInvalidConfigError(err)
	}
	if len(paths) == 0 {
		logger.Debug("no extension paths configured")
		return ctx, result, nil
	}

	baseDir := cfg.GetString("boot.config_dir", "")
	seenPaths := make(map[string]struct{}, len(paths))
	seenComponents := make(map[string]struct{})
	for _, p := range paths {
		resolved := resolvePath(baseDir, p)
		if resolved == "" {
			continue
		}
		if _, ok := seenPaths[resolved]; ok {
			continue
		}
		seenPaths[resolved] = struct{}{}

		logger.Debug("loading extension", zap.String("path", resolved))
		manifest, err := loadManifest(resolved)
		if err != nil {
			logger.Error("extension load failed", zap.String("path", resolved), zap.Error(err))
			return ctx, result, err
		}
		if manifest == nil {
			err := newManifestError(resolved, "manifest is nil")
			logger.Error("extension manifest invalid", zap.String("path", resolved), zap.Error(err))
			return ctx, result, err
		}

		if manifest.ABI != extensionapi.ABI {
			err := newManifestError(resolved, fmt.Sprintf("ABI mismatch (got %d, expected %d)", manifest.ABI, extensionapi.ABI))
			logger.Error("extension ABI mismatch", zap.String("path", resolved), zap.Error(err))
			return ctx, result, err
		}

		next, err := validateAndInitManifest(ctx, manifest, resolved, seenComponents, reserved)
		if err != nil {
			logger.Error("extension validation or init failed", zap.String("path", resolved), zap.Error(err))
			return ctx, result, err
		}
		ctx = next

		name := manifest.Name
		if name == "" {
			name = filepath.Base(resolved)
		}
		result.Extensions = append(result.Extensions, Info{
			Name:    name,
			Version: manifest.Version,
			Path:    resolved,
		})

		if err := appendComponents(&result, manifest, resolved, seenComponents, reserved); err != nil {
			logger.Error("extension component registration failed", zap.String("path", resolved), zap.Error(err))
			return ctx, result, err
		}
		logger.Info("extension loaded", zap.String("name", name), zap.String("version", manifest.Version), zap.String("path", resolved))
	}

	return ctx, result, nil
}

func parsePaths(cfg boot.Config) ([]string, error) {
	val, ok := cfg.Get("paths")
	if !ok || val == nil {
		return nil, nil
	}

	switch v := val.(type) {
	case []string:
		return normalizePaths(v), nil
	case []any:
		paths := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("extensions.paths must be list of strings")
			}
			paths = append(paths, s)
		}
		return normalizePaths(paths), nil
	case string:
		if v == "" {
			return nil, nil
		}
		return normalizePaths([]string{v}), nil
	default:
		return nil, fmt.Errorf("extensions.paths must be list of strings")
	}
}

func resolvePath(baseDir string, path string) string {
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) || baseDir == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func validateAndInitManifest(ctx context.Context, manifest *extensionapi.Manifest, resolved string, seenComponents, reserved map[string]struct{}) (context.Context, error) {
	if err := validateComponents(manifest, resolved, seenComponents, reserved); err != nil {
		return ctx, err
	}
	if manifest.Init == nil {
		return ctx, nil
	}
	next, err := manifest.Init(ctx)
	if err != nil {
		name := manifest.Name
		if name == "" {
			name = filepath.Base(resolved)
		}
		return ctx, newInitError(name, err)
	}
	if next != nil {
		return next, nil
	}
	return ctx, nil
}

func validateComponents(manifest *extensionapi.Manifest, resolved string, seenComponents, reserved map[string]struct{}) error {
	staged := make(map[string]struct{}, len(manifest.Components))
	for _, comp := range manifest.Components {
		if comp == nil {
			continue
		}
		name := comp.Name()
		if name == "" {
			return newManifestError(resolved, "component name is empty")
		}
		if _, exists := reserved[name]; exists {
			return newManifestError(resolved, fmt.Sprintf("component name collides with existing component: %s", name))
		}
		if _, exists := seenComponents[name]; exists {
			return newManifestError(resolved, fmt.Sprintf("duplicate component name: %s", name))
		}
		if _, exists := staged[name]; exists {
			return newManifestError(resolved, fmt.Sprintf("duplicate component name: %s", name))
		}
		staged[name] = struct{}{}
	}
	return nil
}

func appendComponents(
	result *Result,
	manifest *extensionapi.Manifest,
	resolved string,
	seenComponents map[string]struct{},
	reserved map[string]struct{},
) error {
	if manifest == nil || len(manifest.Components) == 0 {
		return nil
	}
	if err := validateComponents(manifest, resolved, seenComponents, reserved); err != nil {
		return err
	}
	for _, comp := range manifest.Components {
		if comp == nil {
			continue
		}
		seenComponents[comp.Name()] = struct{}{}
		result.Components = append(result.Components, comp)
	}
	return nil
}

func loadManifest(path string) (*extensionapi.Manifest, error) {
	p, err := plugin.Open(path)
	if err != nil {
		return nil, newOpenError(path, err)
	}

	sym, err := p.Lookup(extensionapi.Symbol)
	if err != nil {
		return nil, newSymbolError(path, err)
	}

	switch v := sym.(type) {
	case *extensionapi.Manifest:
		if v == nil {
			return nil, newManifestError(path, "manifest is nil")
		}
		return v, nil
	case extensionapi.Manifest:
		return &v, nil
	case func() *extensionapi.Manifest:
		m := v()
		if m == nil {
			return nil, newManifestError(path, "manifest factory returned nil")
		}
		return m, nil
	case func() extensionapi.Manifest:
		m := v()
		return &m, nil
	default:
		return nil, newManifestError(path, "unsupported symbol type")
	}
}
