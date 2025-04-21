package security

import (
	"context"
	"github.com/ponyruntime/pony/api/registry"
)

func Can(ctx context.Context, action, resource string, meta registry.Metadata) bool {

	return true
}
