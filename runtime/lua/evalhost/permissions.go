// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/security"
)

// CheckModulePermissions checks if each module is allowed to be loaded
func CheckModulePermissions(ctx context.Context, modules []string) (string, bool) {
	if len(modules) == 0 {
		return "", true
	}

	meta := attrs.NewBag()
	if frameID, ok := runtime.GetFrameID(ctx); ok {
		meta.Set("entry_id", frameID.String())
	}

	for _, module := range modules {
		if !security.IsAllowed(ctx, "eval.module", module, meta) {
			return module, false
		}
	}
	return "", true
}

// CheckImportPermissions checks if each import is allowed to be loaded
func CheckImportPermissions(ctx context.Context, imports map[string]registry.ID) (string, bool) {
	if len(imports) == 0 {
		return "", true
	}

	meta := attrs.NewBag()
	if frameID, ok := runtime.GetFrameID(ctx); ok {
		meta.Set("entry_id", frameID.String())
	}

	for alias, id := range imports {
		meta.Set("alias", alias)
		if !security.IsAllowed(ctx, "eval.import", id.String(), meta) {
			return id.String(), false
		}
	}
	return "", true
}

// CheckImportModulePermissions verifies the caller may delegate each granted
// module to an import. Granting a privileged module to an import uses the same
// eval.module action as using the module directly, so a caller cannot hand an
// import a capability it is not itself allowed to delegate.
func CheckImportModulePermissions(ctx context.Context, importModules map[string][]string) (string, bool) {
	if len(importModules) == 0 {
		return "", true
	}

	meta := attrs.NewBag()
	if frameID, ok := runtime.GetFrameID(ctx); ok {
		meta.Set("entry_id", frameID.String())
	}

	for alias, mods := range importModules {
		meta.Set("alias", alias)
		for _, module := range mods {
			if !security.IsAllowed(ctx, "eval.module", module, meta) {
				return module, false
			}
		}
	}
	return "", true
}

// CheckClassPermissions checks if each class is allowed to be enabled
func CheckClassPermissions(ctx context.Context, classes []string) (string, bool) {
	if len(classes) == 0 {
		return "", true
	}

	meta := attrs.NewBag()
	if frameID, ok := runtime.GetFrameID(ctx); ok {
		meta.Set("entry_id", frameID.String())
	}

	for _, class := range classes {
		if !security.IsAllowed(ctx, "eval.class", class, meta) {
			return class, false
		}
	}
	return "", true
}
