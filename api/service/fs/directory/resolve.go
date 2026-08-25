// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"context"
	"path"
	"path/filepath"

	"github.com/wippyai/runtime/api/modules"
)

// ResolveDirectory resolves an fs.directory entry's configured path to a load
// path against the source root of the module that owns the entry. The owning
// module is supplied by the caller: the provenance of the operation carrying
// the entry at runtime, the module being built at build time. An empty module
// is host-authored and stays working-directory relative.
func ResolveDirectory(ctx context.Context, moduleName string, cfg *Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.Directory == "" || IsConfiguredPathAbsolute(cfg.Directory) {
		return cfg.Directory
	}
	if cfg.Base == BaseProject {
		return cfg.Directory
	}
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
