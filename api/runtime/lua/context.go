package lua

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// CodeManagerKey is the key used to store/retrieve the code manager in AppContext.
// This key must be shared across all packages that need to access the code manager.
var CodeManagerKey = &ctxapi.Key{Name: "lua.codeManager"}

// CodeManager is the interface for the code manager to avoid circular imports.
// The actual implementation is in runtime/lua/code package.
type CodeManager interface{}

// SetCodeManager stores the code manager in AppContext.
func SetCodeManager(ctx context.Context, cm CodeManager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(CodeManagerKey) == nil {
		ac.With(CodeManagerKey, cm)
	}
	return ctx
}

// GetCodeManager retrieves the code manager from AppContext.
func GetCodeManager(ctx context.Context) CodeManager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	return ac.Get(CodeManagerKey)
}
