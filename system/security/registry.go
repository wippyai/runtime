// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/wippyai/runtime/api/security"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// PolicyRegistry implements the Registry interface to manage security policies
type PolicyRegistry struct {
	ctx          context.Context
	cancel       context.CancelFunc
	bus          event.Bus
	logger       *zap.Logger
	applications map[chan<- error]policyApplication
	subscriber   *eventbus.Subscriber
	policies     sync.Map
	groups       sync.Map
	groupMu      sync.Mutex
	lifecycleMu  sync.Mutex // Serializes Start and the complete Stop barrier.
	mu           sync.Mutex // Lifecycle, application admission and mutation serialization.
}

type policyApplication struct {
	ctx   context.Context
	owner context.Context
}

// NewPolicyRegistry creates a new policy registry with the given event bus and logger
func NewPolicyRegistry(bus event.Bus, logger *zap.Logger) *PolicyRegistry {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &PolicyRegistry{
		bus:          bus,
		logger:       logger,
		policies:     sync.Map{},
		groups:       sync.Map{},
		applications: make(map[chan<- error]policyApplication),
	}
}

func (r *PolicyRegistry) Start(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subscriber != nil {
		return ErrRegistryStarted
	}
	r.ctx, r.cancel = context.WithCancel(ctx)

	sub, err := eventbus.NewSubscriber(
		r.ctx,
		r.bus,
		security.System,
		"policy.(register|update|delete)",
		r.handleEvent,
	)
	if err != nil {
		r.cancel()
		return NewSubscriberError(err)
	}
	r.subscriber = sub

	return nil
}

func (r *PolicyRegistry) Stop() error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	sub := r.subscriber
	r.subscriber = nil
	r.mu.Unlock()
	if sub != nil {
		sub.Close()
	}
	return nil
}

// ApplyPolicy binds the acknowledgement to this owner, not to arbitrary bus
// subscribers. The bus remains the single ordered mutation path.
func (r *PolicyRegistry) ApplyPolicy(ctx context.Context, id registry.ID, kind event.Kind, entry *security.PolicyEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry == nil {
		return ErrInvalidPolicyPayload
	}
	switch kind {
	case security.PolicyRegister, security.PolicyUpdate:
		if entry.Policy == nil || entry.Policy.ID() != id {
			return ErrInvalidPolicyPayload
		}
	case security.PolicyDelete:
	default:
		return ErrInvalidPolicyPayload
	}
	applied := make(chan error, 1)
	r.mu.Lock()
	if r.subscriber == nil || r.ctx.Err() != nil {
		r.mu.Unlock()
		return ErrRegistryStopped
	}
	owner := r.ctx
	r.applications[applied] = policyApplication{ctx: ctx, owner: owner}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.applications, applied)
		r.mu.Unlock()
	}()

	// Snapshot the request without attaching transient waiters to stored policies
	// or modifying the factory's payload.
	payload := &security.PolicyEntry{Applied: applied, Policy: entry.Policy, Groups: slices.Clone(entry.Groups)}
	r.bus.Send(ctx, event.Event{System: security.System, Kind: kind, Path: id.String(), Data: payload})
	var busDone <-chan struct{}
	if lifecycle, ok := r.bus.(interface{ Done() <-chan struct{} }); ok {
		busDone = lifecycle.Done()
	}
	select {
	case err := <-applied:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-owner.Done():
		return ErrRegistryStopped
	case <-busDone:
		return ErrRegistryStopped
	}
}

func (r *PolicyRegistry) handleEvent(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, acknowledged := e.Data.(*security.PolicyEntry)
	acknowledged = acknowledged && entry != nil && entry.Applied != nil
	var err error
	if acknowledged {
		application, ok := r.applications[entry.Applied]
		if !ok {
			// Only the admitting owner may apply or answer this request. This
			// also discards late events after their caller has been released.
			return
		}
		switch {
		case application.owner != r.ctx || application.owner.Err() != nil:
			err = ErrRegistryStopped
		case application.ctx.Err() != nil:
			err = application.ctx.Err()
		}
	}
	if err == nil {
		switch e.Kind {
		case security.PolicyRegister:
			err = r.registerPolicy(e)
		case security.PolicyUpdate:
			err = r.updatePolicy(e)
		case security.PolicyDelete:
			err = r.deletePolicy(e)
		default:
			err = fmt.Errorf("unknown policy event kind: %s", e.Kind)
		}
	}
	if acknowledged {
		// A canceled caller may have gone away, and malformed external events
		// must not block the owner with an unread acknowledgement channel.
		select {
		case entry.Applied <- err:
		default:
		}
	}
	if err != nil {
		r.logger.Error("policy mutation failed", zap.String("policy", e.Path), zap.Error(err))
	}
}

func (r *PolicyRegistry) registerPolicy(e event.Event) error {
	entry, ok := e.Data.(*security.PolicyEntry)
	if !ok || entry == nil || entry.Policy == nil {
		r.logger.Error("invalid policy payload",
			zap.String("policy", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return ErrInvalidPolicyPayload
	}

	policyID := entry.Policy.ID()

	r.policies.Store(policyID, &security.PolicyEntry{Policy: entry.Policy, Groups: slices.Clone(entry.Groups)})

	for _, groupID := range entry.Groups {
		r.addPolicyToGroup(groupID, policyID)
	}

	r.logger.Debug("policy registered",
		zap.String("policy", policyID.String()),
		zap.Int("groups", len(entry.Groups)))
	return nil
}

func (r *PolicyRegistry) updatePolicy(e event.Event) error {
	entry, ok := e.Data.(*security.PolicyEntry)
	if !ok || entry == nil || entry.Policy == nil {
		r.logger.Error("invalid policy update payload",
			zap.String("policy", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return ErrInvalidPolicyPayload
	}

	policyID := entry.Policy.ID()

	existingVal, exists := r.policies.Load(policyID)
	if !exists {
		r.logger.Error("policy not found for update",
			zap.String("policy", policyID.String()))
		return fmt.Errorf("policy not found for update: %s: %w", e.Path, security.ErrPolicyNotFound)
	}

	existing, ok := existingVal.(*security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy type in registry",
			zap.String("policy", policyID.String()))
		return fmt.Errorf("invalid policy type in registry: %s", e.Path)
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

	r.policies.Store(policyID, &security.PolicyEntry{Policy: entry.Policy, Groups: slices.Clone(entry.Groups)})

	r.logger.Debug("policy updated",
		zap.String("policy", policyID.String()),
		zap.Int("groups", len(entry.Groups)))
	return nil
}

func (r *PolicyRegistry) deletePolicy(e event.Event) error {
	policyID := registry.ParseID(e.Path)

	existingVal, exists := r.policies.Load(policyID)
	if !exists {
		r.logger.Warn("policy not found for deletion",
			zap.String("policy", policyID.String()))
		return fmt.Errorf("policy not found for deletion: %s: %w", e.Path, security.ErrPolicyNotFound)
	}

	existing, ok := existingVal.(*security.PolicyEntry)
	if !ok {
		r.logger.Error("invalid policy type in registry",
			zap.String("policy", policyID.String()))
		return fmt.Errorf("invalid policy type in registry: %s", e.Path)
	}

	for _, groupID := range existing.Groups {
		r.removePolicyFromGroup(groupID, policyID)
	}

	r.policies.Delete(policyID)

	r.logger.Debug("policy deleted",
		zap.String("policy", policyID.String()))
	return nil
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
			if policy == nil {
				return nil, fmt.Errorf("policy %s referenced in group %s is nil", id.String(), groupID.String())
			}
			policies = append(policies, policy)
		} else {
			r.logger.Warn("policy referenced in group not found",
				zap.String("group", groupID.String()),
				zap.String("policy", id.String()))
			return nil, fmt.Errorf("policy %s referenced in group %s: %w", id.String(), groupID.String(), err)
		}
	}

	return NewScope(policies), nil
}

func (r *PolicyRegistry) ListGroups() []registry.ID {
	var groups []registry.ID

	r.groups.Range(func(key, _ any) bool {
		if id, ok := key.(registry.ID); ok {
			groups = append(groups, id)
		}
		return true
	})

	return groups
}

func (r *PolicyRegistry) ListPolicies() []registry.ID {
	var policies []registry.ID

	r.policies.Range(func(key, _ any) bool {
		if id, ok := key.(registry.ID); ok {
			policies = append(policies, id)
		}
		return true
	})

	return policies
}

var _ security.Registry = (*PolicyRegistry)(nil)
var _ security.PolicyApplier = (*PolicyRegistry)(nil)
