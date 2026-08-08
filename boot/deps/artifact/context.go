// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var registryKey = &ctxapi.Key{Name: "artifact.registry"}

// WithRegistry stores the boot-composed artifact registry.
func WithRegistry(ctx context.Context, registry *Registry) context.Context {
	appCtx := ctxapi.AppFromContext(ctx)
	if appCtx == nil {
		return ctx
	}
	if appCtx.Get(registryKey) == nil {
		appCtx.With(registryKey, registry)
	}
	return ctx
}

// GetRegistry retrieves the boot-composed artifact registry.
func GetRegistry(ctx context.Context) *Registry {
	appCtx := ctxapi.AppFromContext(ctx)
	if appCtx == nil {
		return nil
	}
	registry, _ := appCtx.Get(registryKey).(*Registry)
	return registry
}
