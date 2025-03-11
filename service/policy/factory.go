package policy

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
)

// FactoryAPI defines the interface for creating policy entries
type FactoryAPI interface {
	// CreatePolicyEntry creates a policy entry from the given registry entry
	CreatePolicyEntry(entry registry.Entry) (*security.PolicyEntry, error)
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
func (f *DefaultFactory) CreatePolicyEntry(entry registry.Entry) (*security.PolicyEntry, error) {
	//// Extract payload from registry entry
	//policyEntry := new(security.PolicyEntry)
	//if err := f.dtt.Unmarshal(entry.Data, policyEntry); err != nil {
	//	return nil, fmt.Errorf("failed to unmarshal policy entry: %w", err)
	//}
	//
	//// Validate policy
	//if policyEntry.Policy == nil {
	//	return nil, fmt.Errorf("policy cannot be nil")
	//}
	//
	//// Normalize group IDs with the entry's namespace
	//for i, groupID := range policyEntry.Groups {
	//	policyEntry.Groups[i] = groupID.WithDefaultNS(entry.ID.NS)
	//}

	// todo: this is wrong!

	return nil, nil
}
