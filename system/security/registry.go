package security

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/security"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// PolicyRegistry implements the Registry interface to manage security policies
type PolicyRegistry struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        event.Bus
	policies   sync.Map // map[registry.ID]PolicyEntry
	groups     sync.Map // map[registry.ID][]registry.ID (group ID -> policy IDs)
	subscriber *eventbus.Subscriber
}

// NewPolicyRegistry creates a new policy registry with the given event bus and logger
func NewPolicyRegistry(bus event.Bus, logger *zap.Logger) *PolicyRegistry {
	return &PolicyRegistry{
		bus:      bus,
		logger:   logger,
		policies: sync.Map{},
		groups:   sync.Map{},
	}
}

// Start begins listening for policy registration events
func (r *PolicyRegistry) Start(ctx context.Context) error {
	r.ctx = ctx

	// Subscribe to policy events
	sub, err := eventbus.NewSubscriber(
		r.ctx,
		r.bus,
		security.System,
		"security.policy.(register|update|delete)",
		r.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create policy subscriber: %w", err)
	}
	r.subscriber = sub

	return nil
}

// Stop cleanly shuts down the registry
func (r *PolicyRegistry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *PolicyRegistry) handleEvent(e event.Event) {
	switch e.Kind {
	case security.PolicyRegister:
		r.registerPolicy(e)
	case security.PolicyUpdate:
		r.updatePolicy(e)
	case security.PolicyDelete:
		r.deletePolicy(e)
	default:
		r.logger.Warn("unknown policy event kind",
			zap.String("kind", string(e.Kind)),
			zap.String("path", string(e.Path)))
	}
}

func (r *PolicyRegistry) registerPolicy(e event.Event) {
	entry, ok := e.Data.(*security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy payload",
			zap.String("policy", string(e.Path)),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	policyID := entry.Policy.ID()

	// Store the policy entry directly
	r.policies.Store(policyID, entry)

	// Add policy to its groups
	for _, groupID := range entry.Groups {
		r.addPolicyToGroup(groupID, policyID)
	}

	r.logger.Debug("policy registered",
		zap.String("policy", policyID.String()),
		zap.Int("groups", len(entry.Groups)))
}

func (r *PolicyRegistry) updatePolicy(e event.Event) {
	entry, ok := e.Data.(security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy update payload",
			zap.String("policy", string(e.Path)),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	policyID := entry.Policy.ID()

	// Check if policy exists
	existingVal, exists := r.policies.Load(policyID)
	if !exists {
		r.logger.Error("policy not found for update",
			zap.String("policy", policyID.String()))
		return
	}

	existing := existingVal.(*security.PolicyEntry)

	// Remove from old groups that aren't in the new list
	for _, oldGroup := range existing.Groups {
		found := false
		for _, newGroup := range entry.Groups {
			if oldGroup == newGroup {
				found = true
				break
			}
		}
		if !found {
			r.removePolicyFromGroup(oldGroup, policyID)
		}
	}

	// Add to new groups that weren't in the old list
	for _, newGroup := range entry.Groups {
		found := false
		for _, oldGroup := range existing.Groups {
			if oldGroup == newGroup {
				found = true
				break
			}
		}
		if !found {
			r.addPolicyToGroup(newGroup, policyID)
		}
	}

	// Update policy
	r.policies.Store(policyID, entry)

	r.logger.Debug("policy updated",
		zap.String("policy", policyID.String()),
		zap.Int("groups", len(entry.Groups)))
}

func (r *PolicyRegistry) deletePolicy(e event.Event) {
	policyID := registry.ParseID(e.Path)

	// Get policy info to remove from groups
	existingVal, exists := r.policies.Load(policyID)
	if !exists {
		r.logger.Warn("policy not found for deletion",
			zap.String("policy", policyID.String()))
		return
	}

	// Remove from all groups
	existing := existingVal.(*security.PolicyEntry)
	for _, groupID := range existing.Groups {
		r.removePolicyFromGroup(groupID, policyID)
	}

	// Remove policy
	r.policies.Delete(policyID)

	r.logger.Debug("policy deleted",
		zap.String("policy", policyID.String()))
}

// addPolicyToGroup adds a policy to a group, creating the group if it doesn't exist
func (r *PolicyRegistry) addPolicyToGroup(groupID, policyID registry.ID) {
	var groupPolicies []registry.ID

	// Get existing group or create new
	if val, ok := r.groups.Load(groupID); ok {
		groupPolicies = val.([]registry.ID)

		// Check if policy is already in group
		for _, id := range groupPolicies {
			if id == policyID {
				return // Policy already in group
			}
		}
	}

	// Add policy to group
	groupPolicies = append(groupPolicies, policyID)
	r.groups.Store(groupID, groupPolicies)
}

// removePolicyFromGroup removes a policy from a group
func (r *PolicyRegistry) removePolicyFromGroup(groupID, policyID registry.ID) {
	val, ok := r.groups.Load(groupID)
	if !ok {
		return // Group doesn't exist
	}

	groupPolicies := val.([]registry.ID)

	// Create new slice without the policy
	newGroupPolicies := make([]registry.ID, 0, len(groupPolicies))
	for _, id := range groupPolicies {
		if id != policyID {
			newGroupPolicies = append(newGroupPolicies, id)
		}
	}

	// Update or remove the group
	if len(newGroupPolicies) > 0 {
		r.groups.Store(groupID, newGroupPolicies)
	} else {
		r.groups.Delete(groupID)
	}
}

// GetPolicy retrieves a policy by its ID
func (r *PolicyRegistry) GetPolicy(id registry.ID) (security.Policy, error) {
	val, ok := r.policies.Load(id)
	if !ok {
		return nil, security.ErrPolicyNotFound
	}

	entry := val.(*security.PolicyEntry)
	return entry.Policy, nil
}

// GetPolicyGroup retrieves all policies in a group as a scope
func (r *PolicyRegistry) GetPolicyGroup(groupID registry.ID) (security.Scope, error) {
	val, ok := r.groups.Load(groupID)
	if !ok {
		return nil, security.ErrGroupNotFound
	}

	policyIDs := val.([]registry.ID)
	policies := make([]security.Policy, 0, len(policyIDs))

	// Collect all policies in the group
	for _, id := range policyIDs {
		if policy, err := r.GetPolicy(id); err == nil {
			policies = append(policies, policy)
		} else {
			r.logger.Warn("policy referenced in group not found",
				zap.String("group", groupID.String()),
				zap.String("policy", id.String()))
		}
	}

	// Create a new scope with all the policies
	return NewScope(policies), nil
}

// ListGroups returns all available policy group IDs
func (r *PolicyRegistry) ListGroups() []registry.ID {
	var groups []registry.ID

	r.groups.Range(func(key, _ interface{}) bool {
		groups = append(groups, key.(registry.ID))
		return true
	})

	return groups
}

// ListPolicies returns all available policy IDs
func (r *PolicyRegistry) ListPolicies() []registry.ID {
	var policies []registry.ID

	r.policies.Range(func(key, _ interface{}) bool {
		policies = append(policies, key.(registry.ID))
		return true
	})

	return policies
}

// Ensure PolicyRegistry implements the Registry interface
var _ security.Registry = (*PolicyRegistry)(nil)
