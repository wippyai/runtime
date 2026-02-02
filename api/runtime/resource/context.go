package resource

import (
	"context"
	"io"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// ReaderProvider is implemented by types that can provide an io.Reader from context.
// This allows streams and other abstractions to be used as input sources without
// requiring direct type coupling.
type ReaderProvider interface {
	GetReader(ctx context.Context) (io.Reader, error)
}

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
