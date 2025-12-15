package security

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/security"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
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
	groupMu    sync.Mutex // protects group slice modifications
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

func (r *PolicyRegistry) Start(ctx context.Context) error {
	r.ctx = ctx

	sub, err := eventbus.NewSubscriber(
		r.ctx,
		r.bus,
		security.System,
		"policy.(register|update|delete)",
		r.handleEvent,
	)
	if err != nil {
		return NewSubscriberError(err)
	}
	r.subscriber = sub

	return nil
}

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
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *PolicyRegistry) registerPolicy(e event.Event) {
	entry, ok := e.Data.(*security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy payload",
			zap.String("policy", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	policyID := entry.Policy.ID()

	r.policies.Store(policyID, entry)

	for _, groupID := range entry.Groups {
		r.addPolicyToGroup(groupID, policyID)
	}

	r.logger.Debug("policy registered",
		zap.String("policy", policyID.String()),
		zap.Int("groups", len(entry.Groups)))
}

func (r *PolicyRegistry) updatePolicy(e event.Event) {
	entry, ok := e.Data.(*security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy update payload",
			zap.String("policy", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	policyID := entry.Policy.ID()

	existingVal, exists := r.policies.Load(policyID)
	if !exists {
		r.logger.Error("policy not found for update",
			zap.String("policy", policyID.String()))
		return
	}

	existing, ok := existingVal.(*security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy type in registry",
			zap.String("policy", policyID.String()))
		return
	}

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

	r.policies.Store(policyID, entry)

	r.logger.Debug("policy updated",
		zap.String("policy", policyID.String()),
		zap.Int("groups", len(entry.Groups)))
}

func (r *PolicyRegistry) deletePolicy(e event.Event) {
	policyID := registry.ParseID(e.Path)

	existingVal, exists := r.policies.Load(policyID)
	if !exists {
		r.logger.Warn("policy not found for deletion",
			zap.String("policy", policyID.String()))
		return
	}

	existing, ok := existingVal.(*security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy type in registry",
			zap.String("policy", policyID.String()))
		return
	}

	for _, groupID := range existing.Groups {
		r.removePolicyFromGroup(groupID, policyID)
	}

	r.policies.Delete(policyID)

	r.logger.Debug("policy deleted",
		zap.String("policy", policyID.String()))
}

func (r *PolicyRegistry) addPolicyToGroup(groupID, policyID registry.ID) {
	r.groupMu.Lock()
	defer r.groupMu.Unlock()

	var groupPolicies []registry.ID

	if val, ok := r.groups.Load(groupID); ok {
		groupPolicies, ok = val.([]registry.ID)
		if !ok {
			r.logger.Error("invalid group type in registry",
				zap.String("group", groupID.String()))
			return
		}

		for _, id := range groupPolicies {
			if id == policyID {
				return
			}
		}
	}

	groupPolicies = append(groupPolicies, policyID)
	r.groups.Store(groupID, groupPolicies)
}

func (r *PolicyRegistry) removePolicyFromGroup(groupID, policyID registry.ID) {
	r.groupMu.Lock()
	defer r.groupMu.Unlock()

	val, ok := r.groups.Load(groupID)
	if !ok {
		return
	}

	groupPolicies, ok := val.([]registry.ID)
	if !ok {
		r.logger.Error("invalid group type in registry",
			zap.String("group", groupID.String()))
		return
	}

	newGroupPolicies := make([]registry.ID, 0, len(groupPolicies))
	for _, id := range groupPolicies {
		if id != policyID {
			newGroupPolicies = append(newGroupPolicies, id)
		}
	}

	if len(newGroupPolicies) > 0 {
		r.groups.Store(groupID, newGroupPolicies)
	} else {
		r.groups.Delete(groupID)
	}
}

func (r *PolicyRegistry) GetPolicy(id registry.ID) (security.Policy, error) {
	val, ok := r.policies.Load(id)
	if !ok {
		return nil, security.ErrPolicyNotFound
	}

	entry, ok := val.(*security.PolicyEntry)
	if !ok {
		return nil, security.ErrPolicyNotFound
	}
	return entry.Policy, nil
}

func (r *PolicyRegistry) GetPolicyGroup(groupID registry.ID) (security.Scope, error) {
	val, ok := r.groups.Load(groupID)
	if !ok {
		return nil, security.ErrGroupNotFound
	}

	policyIDs, ok := val.([]registry.ID)
	if !ok {
		return nil, security.ErrGroupNotFound
	}
	policies := make([]security.Policy, 0, len(policyIDs))

	for _, id := range policyIDs {
		if policy, err := r.GetPolicy(id); err == nil {
			policies = append(policies, policy)
		} else {
			r.logger.Warn("policy referenced in group not found",
				zap.String("group", groupID.String()),
				zap.String("policy", id.String()))
		}
	}

	return NewScope(policies), nil
}

func (r *PolicyRegistry) ListGroups() []registry.ID {
	var groups []registry.ID

	r.groups.Range(func(key, _ interface{}) bool {
		if id, ok := key.(registry.ID); ok {
			groups = append(groups, id)
		}
		return true
	})

	return groups
}

func (r *PolicyRegistry) ListPolicies() []registry.ID {
	var policies []registry.ID

	r.policies.Range(func(key, _ interface{}) bool {
		if id, ok := key.(registry.ID); ok {
			policies = append(policies, id)
		}
		return true
	})

	return policies
}

var _ security.Registry = (*PolicyRegistry)(nil)
