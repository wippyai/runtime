package resource

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// StoreKey is the context key for Store in FrameContext.
var StoreKey = &ctxapi.Key{Name: "resource.store", Inherit: false}

// SetStore stores Store in FrameContext.
func SetStore(ctx context.Context, s *Store) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(StoreKey, s)
}

// GetStore retrieves Store from FrameContext.
func GetStore(ctx context.Context) *Store {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(StoreKey); ok {
		return val.(*Store)
	}
	return nil
}

// GetTable retrieves Table from FrameContext via Store.
func GetTable(ctx context.Context) *Table {
	s := GetStore(ctx)
	if s == nil {
		return nil
	}
	return s.Table()
}
