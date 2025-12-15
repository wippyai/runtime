// Package policy provides policy service configuration.
package policy

import (
	"errors"

	"github.com/wippyai/runtime/api/registry"
)

// ErrFieldNotFound is returned when a field path cannot be resolved.
var ErrFieldNotFound = errors.New("field not found")

const (
	// Policy represents the kind of policy entries in the registry
	Policy registry.Kind = "security.policy"
)

// Condition represents a policy condition
type Condition struct {
	// Field is the path to the field to evaluate (e.g., "actor.meta.role", "meta.owner")
	Field string `json:"field"`

	// Operator defines the comparison operation (e.g., "eq", "lt", "gt", "in", "exists")
	Operator string `json:"operator"`

	// Value is the static value to compare against
	Value any `json:"value,omitempty"`

	// ValueFrom is a reference to another field (e.g., "actor.id")
	ValueFrom string `json:"value_from,omitempty"`
}

// Effect represents the policy effect (allow or deny)
type Effect string

const (
	// Allow grants access
	Allow Effect = "allow"

	// Deny denies access
	Deny Effect = "deny"
)

// Definition represents the policy configuration
type Definition struct {
	// Actions defines the actions this policy applies to.
	// Can be a list of specific actions or "*" for all.
	Actions any `json:"actions"`

	// Resources defines the resources this policy applies to.
	// Can be a list of specific resources or "*" for all.
	Resources any `json:"resources"`

	// Conditions defines the conditions that must be met for the policy to apply
	Conditions []Condition `json:"conditions,omitempty"`

	// Effect determines whether the policy allows or denies access
	Effect Effect `json:"effect"`
}

// Config represents a security policy entry configuration
type Config struct {
	// Policy contains the core policy rules
	Policy Definition `json:"policy"`

	// Groups defines the group IDs this policy belongs to
	Groups []string `json:"groups,omitempty"`
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validate policy effect
	if c.Policy.Effect != Allow && c.Policy.Effect != Deny {
		return NewInvalidPolicyEffectError(c.Policy.Effect)
	}

	// Validate actions
	switch actions := c.Policy.Actions.(type) {
	case string:
		// Allow any non-empty string (including "*" or pattern wildcards)
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

	// Validate resources - similar changes
	switch resources := c.Policy.Resources.(type) {
	case string:
		// Allow any non-empty string (including "*" or pattern wildcards)
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

	// Validate conditions
	for i, condition := range c.Policy.Conditions {
		if condition.Field == "" {
			return NewConditionFieldEmptyError(i)
		}

		if condition.Operator == "" {
			return NewConditionOperatorEmptyError(i)
		}

		if condition.Value == nil && condition.ValueFrom == "" {
			return NewConditionValueRequiredError(i)
		}

		// Validate operators
		switch condition.Operator {
		case "eq", "ne", "lt", "gt", "lte", "gte",
			"in", "nin",
			"exists", "nexists",
			"contains", "ncontains",
			"matches", "nmatches":
			// Valid operators
		default:
			return NewConditionInvalidOperatorError(i, condition.Operator)
		}
	}

	return nil
}

// GetActions returns the actions as a string slice
func (c *Config) GetActions() []string {
	switch actions := c.Policy.Actions.(type) {
	case string:
		if actions == "*" {
			return []string{"*"}
		}
		return []string{actions}
	case []any:
		result := make([]string, 0, len(actions))
		for _, action := range actions {
			if str, ok := action.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return []string{}
}

// GetResources returns the resources as a string slice
func (c *Config) GetResources() []string {
	switch resources := c.Policy.Resources.(type) {
	case string:
		if resources == "*" {
			return []string{"*"}
		}
		return []string{resources}
	case []any:
		result := make([]string, 0, len(resources))
		for _, resource := range resources {
			if str, ok := resource.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	return []string{}
}

// GetGroupIDs returns the groups as registry.ID slice
func (c *Config) GetGroupIDs(defaultNS registry.Namespace) []registry.ID {
	result := make([]registry.ID, 0, len(c.Groups))
	for _, group := range c.Groups {
		id := registry.ParseID(group)
		ns := id.NS
		if ns == "" {
			ns = defaultNS
		}
		result = append(result, registry.NewID(ns, id.Name))
	}
	return result
}
