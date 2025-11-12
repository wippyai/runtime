package tokenstore

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/service/tokenstore"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/security"
	entryutil "github.com/ponyruntime/pony/internal/entry"
)

// Factory creates token stores from configuration
type Factory struct {
	dtt              payload.Transcoder
	resourceRegistry resource.Registry
	securityRegistry security.Registry
}

// NewFactory creates a new token store factory
func NewFactory(
	dtt payload.Transcoder,
	resourceRegistry resource.Registry,
	securityRegistry security.Registry,
) *Factory {
	return &Factory{
		dtt:              dtt,
		resourceRegistry: resourceRegistry,
		securityRegistry: securityRegistry,
	}
}

// CreateTokenStore creates a token store from a registry entry
func (f *Factory) CreateTokenStore(ctx context.Context, entry registry.Entry) (security.TokenStore, error) {
	// Decode configuration
	cfg, err := entryutil.DecodeEntryConfig[tokenstore.Config](ctx, f.dtt, entry)
	if err != nil {
		return nil, fmt.Errorf("failed to decode token store config: %w", err)
	}

	// Create token store with lazy loading capability
	tokenStore, err := NewStoreTokenStore(cfg, f.dtt, f.resourceRegistry, f.securityRegistry)
	if err != nil {
		return nil, err
	}

	return tokenStore, nil
}
