<!-- SPDX-License-Identifier: MPL-2.0 -->

# Security Policy Writing Guide

## Introduction

Security policies control access to resources in your system. Each policy defines who can perform what actions on which resources. This guide explains how to write effective security policies.

## Policy Structure in YAML

Policies are defined within a YAML configuration file following this structure:

```yaml
version: "1.0"
namespace: system.security

entries:
  - name: policy_name
    kind: security.policy
    meta:
      comment: "Policy description"
    policy:
      actions: "<action or actions>"
      resources: "<resource or resources>"
      effect: "<allow|deny>"
      conditions:
        - field: "<field path>"
          operator: "<operator>"
          value: "<value>"
    groups: ["group1", "group2"]  # Policy groups
```

## Key Components

- **name**: Unique identifier for the policy
- **kind**: Must be `security.policy`
- **meta.comment**: Description of the policy's purpose
- **policy.actions**: Specifies which actions this policy governs
- **policy.resources**: Defines which resources this policy applies to
- **policy.effect**: Either "allow" or "deny"
- **policy.conditions**: Optional rules that must be met for the policy to apply
- **groups**: Optional list of policy groups this policy belongs to

## Actions and Resources

Both actions and resources can be specified in several ways:

### Global Wildcard
Use `"*"` to match everything:
```yaml
actions: "*"
resources: "*"
```

### Pattern Matching
Use patterns ending with `.*` or `*`:
```yaml
actions: "document.read.*"
resources: "document:*"
```

### Specific Values
Define exact action or resource names:
```yaml
actions: "document.read"
resources: "document:financial"
```

### Lists
Define arrays of values:
```yaml
actions:
  - "document.read"
  - "document.update"
  - "document.delete"
resources:
  - "document:financial"
  - "document:legal"
```

## Conditions

Conditions determine when a policy applies. Each condition has:

- **field**: Path to evaluate (using dot notation)
- **operator**: Comparison operation
- **value**: Static value to compare against, or
- **value_from**: Reference to another field

### Field Paths

- `actor.id`: ID of the actor
- `actor.meta.*`: Actor metadata (e.g., `actor.meta.role`)
- `meta.*`: Resource metadata
- `action`: The action being performed
- `resource`: The resource being accessed

### Available Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `eq` | Equals | `field: "actor.meta.role"` <br> `operator: "eq"` <br> `value: "admin"` |
| `ne` | Not equals | `field: "resource"` <br> `operator: "ne"` <br> `value: "restricted"` |
| `lt` | Less than | `field: "meta.age"` <br> `operator: "lt"` <br> `value: 18` |
| `gt` | Greater than | `field: "meta.priority"` <br> `operator: "gt"` <br> `value: 5` |
| `in` | In array | `field: "action"` <br> `operator: "in"` <br> `value: ["read", "list"]` |
| `exists` | Field exists | `field: "meta.owner"` <br> `operator: "exists"` <br> `value: true` |
| `contains` | Contains substring | `field: "resource"` <br> `operator: "contains"` <br> `value: "sensitive"` |
| `matches` | Regex match | `field: "resource"` <br> `operator: "matches"` <br> `value: "^doc.*$"` |

## Policy Examples

### Admin Access Policy
```yaml
- name: admin_policy
  kind: security.policy
  meta:
    comment: "Global admin access policy"
  policy:
    actions: "*"
    resources: "*"
    effect: "allow"
    conditions:
      - field: "actor.meta.role"
        operator: "eq"
        value: "admin"
  groups: ["admin"]
```

### Resource Owner Policy
```yaml
- name: resource_owner_policy
  kind: security.policy
  meta:
    comment: "Resource owner access policy"
  policy:
    actions:
      - "document.read"
      - "document.update"
      - "document.delete"
    resources: "document:*"
    effect: "allow"
    conditions:
      - field: "meta.owner"
        operator: "eq"
        value_from: "actor.id"
  groups: ["default"]
```

### Deny Policy for Sensitive Resources
```yaml
- name: sensitive_deny_policy
  kind: security.policy
  meta:
    comment: "Deny access to sensitive documents"
  policy:
    actions: "*"
    resources: "document:*"
    effect: "deny"
    conditions:
      - field: "meta.classification"
        operator: "eq"
        value: "confidential"
      - field: "actor.meta.clearance"
        operator: "lt"
        value: 3
  groups: ["security"]
```

## Evaluation Logic

When multiple policies apply:

1. If any policy explicitly denies an action, it is denied (deny overrides allow)
2. If no denials and at least one policy allows, it is allowed
3. If no applicable policies, access is denied by default

## Best Practices

1. **Start restrictive**: Begin with minimal permissions and add as needed
2. **Use groups**: Organize policies into logical groups
3. **Leverage conditions**: Create fine-grained access control with conditions
4. **Review regularly**: Audit policies periodically to ensure they remain appropriate
5. **Document policies**: Add clear comments explaining each policy's purpose