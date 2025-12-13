package policy

import "github.com/wippyai/runtime/api/registry"

const (
	// ExprKind is the registry kind for expression-based policies
	ExprKind registry.Kind = "security.policy.expr"
)

// ExprDefinition represents an expression-based policy definition
type ExprDefinition struct {
	// Actions defines which actions this policy applies to (string or []string)
	// Use "*" for all actions
	Actions any `json:"actions" yaml:"actions"`

	// Resources defines which resources this policy applies to (string or []string)
	// Use "*" for all resources
	Resources any `json:"resources" yaml:"resources"`

	// Expression is the expr-lang expression that evaluates to true/false
	// The expression has access to: actor, action, resource, meta
	// Example: actor.meta.role == "admin" || (action == "read" && meta.public)
	Expression string `json:"expression" yaml:"expression"`

	// Effect determines the policy result when the expression is true
	Effect Effect `json:"effect" yaml:"effect"`
}

// ExprConfig represents the configuration for an expression-based policy
type ExprConfig struct {
	// Policy is the policy definition
	Policy ExprDefinition `json:"policy" yaml:"policy"`

	// Groups is a list of group names this policy belongs to
	// Groups are namespaced with the entry's namespace
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`
}

// Validate checks if the configuration is valid
func (c *ExprConfig) Validate() error {
	// Validate policy effect
	if c.Policy.Effect != Allow && c.Policy.Effect != Deny {
		return NewInvalidPolicyEffectError(c.Policy.Effect)
	}

	// Validate actions
	switch actions := c.Policy.Actions.(type) {
	case string:
		if actions == "" {
			return ErrActionsStringEmpty
		}
	case []any:
		if len(actions) == 0 {
			return ErrActionsListEmpty
		}
	default:
		return ErrActionsInvalidType
	}

	// Validate resources
	switch resources := c.Policy.Resources.(type) {
	case string:
		if resources == "" {
			return ErrResourcesStringEmpty
		}
	case []any:
		if len(resources) == 0 {
			return ErrResourcesListEmpty
		}
	default:
		return ErrResourcesInvalidType
	}

	// Validate expression
	if c.Policy.Expression == "" {
		return ErrExpressionEmpty
	}

	return nil
}

// GetGroupIDs converts group names to fully qualified registry IDs
func (c *ExprConfig) GetGroupIDs(namespace registry.Namespace) []registry.ID {
	ids := make([]registry.ID, len(c.Groups))
	for i, group := range c.Groups {
		ids[i] = registry.NewID(namespace, group)
	}
	return ids
}
