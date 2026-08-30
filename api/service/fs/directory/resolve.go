// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"context"
	"path"
	"path/filepath"

	"github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/registry"
)

// ResolveDirectory resolves an fs.directory entry's configured path to a load path.
func ResolveDirectory(ctx context.Context, entry registry.Entry, cfg *Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.Directory == "" || IsConfiguredPathAbsolute(cfg.Directory) {
		return cfg.Directory
	}
	if cfg.Base == BaseProject {
		return cfg.Directory
	}

	moduleName := entry.Registry.Owner
	if moduleName == "" {
		return cfg.Directory
	}

	sourceRegistry := modules.GetSourceRegistry(ctx)
	if sourceRegistry == nil {
		return cfg.Directory
	}
	root, ok := sourceRegistry.ResourceRoot(moduleName)
	if !ok {
		return cfg.Directory
	}

	return filepath.Join(root, cfg.Directory)
}

// IsConfiguredPathAbsolute reports whether a configured directory path is absolute,
// in host or slash form.
func IsConfiguredPathAbsolute(dir string) bool {
	return filepath.IsAbs(dir) || path.IsAbs(filepath.ToSlash(dir))
}
