package tokenstore

import (
	"context"

	tokenstoreapi "github.com/wippyai/runtime/api/service/security/tokenstore"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
	entryutil "github.com/wippyai/runtime/internal/entry"
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
	cfg, err := entryutil.DecodeEntryConfig[tokenstoreapi.Config](ctx, f.dtt, entry)
	if err != nil {
		return nil, tokenstoreapi.NewDecodeTokenStoreConfigError(err)
	}

	// Create token store with lazy loading capability
	tokenStore, err := NewStoreTokenStore(cfg, f.dtt, f.resourceRegistry, f.securityRegistry)
	if err != nil {
		return nil, err
	}

	return tokenStore, nil
}
