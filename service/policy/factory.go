package policy

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/api/service/policy"
	entryutil "github.com/ponyruntime/pony/internal/entry"
)

// FactoryAPI defines the interface for creating policy entries
type FactoryAPI interface {
	// CreatePolicyEntry creates a policy entry from the given registry entry
	CreatePolicyEntry(ctx context.Context, entry registry.Entry) (*security.PolicyEntry, error)
}

// DefaultFactory is the default implementation of FactoryAPI
type DefaultFactory struct {
	dtt payload.Transcoder
}

// NewDefaultFactory creates a new default policy entry factory
func NewDefaultFactory(dtt payload.Transcoder) *DefaultFactory {
	return &DefaultFactory{
		dtt: dtt,
	}
}

// CreatePolicyEntry implements FactoryAPI
func (f *DefaultFactory) CreatePolicyEntry(ctx context.Context, entry registry.Entry) (*security.PolicyEntry, error) {
	// Extract payload from registry entry
	cfg, err := entryutil.DecodeEntryConfig[policy.Config](ctx, f.dtt, entry)
	if err != nil {
		return nil, fmt.Errorf("failed to decode policy config: %w", err)
	}

	// Create the policy
	policyObj, err := NewPolicy(entry.ID, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy: %w", err)
	}

	// Create policy entry
	policyEntry := &security.PolicyEntry{
		Policy: policyObj,
		Groups: cfg.GetGroupIDs(entry.ID.NS),
	}

	return policyEntry, nil
}
