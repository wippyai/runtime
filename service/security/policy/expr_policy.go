package policy

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	policyapi "github.com/wippyai/runtime/api/service/security/policy"
)

// ExprPolicy implements Policy using expr-lang expressions
type ExprPolicy struct {
	id        registry.ID
	config    *policyapi.ExprConfig
	evaluator *ExprEvaluator
}

// NewExprPolicy creates a new expression-based policy
func NewExprPolicy(id registry.ID, config *policyapi.ExprConfig) (*ExprPolicy, error) {
	if config == nil {
		return nil, ErrConfigNil
	}

	// Create evaluator with pre-compiled expression
	evaluator, err := NewExprEvaluator(config.Policy.Expression)
	if err != nil {
		return nil, NewCompileExpressionError(err)
	}

	return &ExprPolicy{
		id:        id,
		config:    config,
		evaluator: evaluator,
	}, nil
}

// ID returns the policy's unique identifier
func (p *ExprPolicy) ID() registry.ID {
	return p.id
}

// Evaluate determines if the action on resource is allowed/denied
func (p *ExprPolicy) Evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	// Check if policy applies to this action
	if !p.matchesActions(action) {
		return security.Undefined
	}

	// Check if policy applies to this resource
	if !p.matchesResources(resource) {
		return security.Undefined
	}

	// Build environment for expression evaluation
	env := map[string]any{
		"actor":    actor,
		"action":   action,
		"resource": resource,
		"meta":     meta,
	}

	// Evaluate expression
	matches, err := p.evaluator.Evaluate(env)
	if err != nil {
		// Expression evaluation error - treat as undefined (policy doesn't apply)
		return security.Undefined
	}

	// If expression doesn't match, policy doesn't apply
	if !matches {
		return security.Undefined
	}

	// Expression matched - return configured effect
	switch p.config.Policy.Effect {
	case policyapi.Allow:
		return security.Allow
	case policyapi.Deny:
		return security.Deny
	default:
		return security.Undefined
	}
}

// matchesActions checks if the policy applies to the given action
func (p *ExprPolicy) matchesActions(action string) bool {
	return matchesFilter(p.config.Policy.Actions, action)
}

// matchesResources checks if the policy applies to the given resource
func (p *ExprPolicy) matchesResources(resource string) bool {
	return matchesFilter(p.config.Policy.Resources, resource)
}

var _ security.Policy = (*ExprPolicy)(nil)
