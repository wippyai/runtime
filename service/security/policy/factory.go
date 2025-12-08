package policy

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/service/security/policy"
	entryutil "github.com/wippyai/runtime/internal/entry"
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
	switch entry.Kind {
	case policy.Kind:
		return f.createConditionPolicy(ctx, entry)
	case policy.ExprKind:
		return f.createExprPolicy(ctx, entry)
	default:
		return nil, NewUnsupportedPolicyKindError(entry.Kind)
	}
}

// createConditionPolicy creates a condition-based policy
func (f *DefaultFactory) createConditionPolicy(ctx context.Context, entry registry.Entry) (*security.PolicyEntry, error) {
	// Extract payload from registry entry
	cfg, err := entryutil.DecodeEntryConfig[policy.Config](ctx, f.dtt, entry)
	if err != nil {
		return nil, NewDecodePolicyConfigError(err)
	}

	// Create the policy
	policyObj, err := NewPolicy(entry.ID, cfg)
	if err != nil {
		return nil, NewCreatePolicyError(err)
	}

	// Create policy entry
	policyEntry := &security.PolicyEntry{
		Policy: policyObj,
		Groups: cfg.GetGroupIDs(entry.ID.NS),
	}

	return policyEntry, nil
}

// createExprPolicy creates an expression-based policy
func (f *DefaultFactory) createExprPolicy(ctx context.Context, entry registry.Entry) (*security.PolicyEntry, error) {
	// Extract payload from registry entry
	cfg, err := entryutil.DecodeEntryConfig[policy.ExprConfig](ctx, f.dtt, entry)
	if err != nil {
		return nil, NewDecodeExprPolicyConfigError(err)
	}

	// Create the policy
	policyObj, err := NewExprPolicy(entry.ID, cfg)
	if err != nil {
		return nil, NewCreateExprPolicyError(err)
	}

	// Create policy entry
	policyEntry := &security.PolicyEntry{
		Policy: policyObj,
		Groups: cfg.GetGroupIDs(entry.ID.NS),
	}

	return policyEntry, nil
}
