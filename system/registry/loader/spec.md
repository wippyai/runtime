# Core Application Component Specification v1.0

## 1. File Structure

### 1.1 Configuration Files

- Supported formats: YAML (.yaml, .yml) and JSON (.json)
- File naming: lowercase with hyphens (e.g., `api-components.yaml`)
- Directory structure should reflect component organization

### 1.2 Basic Structure

```yaml
version: "1.0"                      # Optional, file version
namespace: "company.product.env"    # Required, namespace using dots or slashes
                                   # Examples: "company/product/env" or "company.product.env"

meta: { ... }                      # Optional, shared metadata
entries: [ ... ]                   # Optional, multiple components
```

Or single component format:

```yaml
version: "1.0"                      # Optional, file version
namespace: "company.product.env"    # Required, namespace using dots or slashes

name: "component.path"              # Required when not using entries
kind: "component.type"              # Required
meta: { ... }                      # Optional

# Component specific configuration
```

### 1.3 Directory Organization

```
config/
  ├── components/
  │   ├── group-a/
  │   │   ├── component-1.yaml
  │   │   └── component-2.yaml
  │   └── group-b/
  │       └── component-3.yaml
  └── environments/
      ├── dev/
      └── prod/
```

## 2. Naming and References

### 2.1 Separator Rules

- Use either `/` or `.` for hierarchical paths:
   - Namespace hierarchy
   - Component paths
- Use `:` only as a system separator between namespace and component reference
- Cannot use `:` in namespace or component names

### 2.2 Naming Patterns

```
Basic naming:         ^[a-z0-9]+([/.][a-z0-9]+)*$
Component path:       ^[a-z0-9]+([/.][a-z0-9]+)*$
Fully qualified:     ^[a-z0-9]+([/.][a-z0-9]+)*:[a-z0-9]+([/.][a-z0-9]+)*$
```

Examples of valid names:
```yaml
namespace: company/product/env
namespace: company.product.env
name: service/api/v1
name: service.api.v1
reference: company.product.env:service.api.v1
reference: company/product/env:service/api/v1
```

### 2.3 Reference Format

```yaml
meta:
  dependency: "company.product.env:component.path"   # Full reference
  depends_on: # Dependencies
    - "company/product/env:component/path"          # Direct reference (slash style)
    - "other.product.env:component.path"            # Cross-namespace (dot style)
    - "group:service.type"                          # Group reference
    - "ns:company.product"                          # Namespace reference
```

## 3. Meta Fields

### 3.1 Standard Fields

```yaml
meta:
  depends_on: [ ]      # Component dependencies
  groups: [ ]          # Group memberships
  labels: { }          # Custom labels
```

### 3.2 Field Rules

- All values must be strings
- Arrays must contain only strings
- References must be fully qualified
- Labels must have string values
- No nested objects in meta

## 4. Dependencies

### 4.1 Dependency Types

1. Direct Dependencies
   ```yaml
   depends_on: ["company.product.env:component.path"]
   ```

2. Group Dependencies
   ```yaml
   depends_on: ["group:service.type"]
   ```

3. Namespace Dependencies
   ```yaml
   depends_on: ["ns:company.product"]
   ```

### 4.2 Resolution Rules

- Direct dependencies are resolved first
- Group dependencies are resolved second
- Namespace dependencies are resolved last
- Within each type: topological sort
- Circular dependencies are not allowed

## 5. Loading Process

### 5.1 Load Sequence

1. File Discovery
    - Recursive directory scan
    - Format identification
    - Initial validation

2. Content Processing
    - Parse file content
    - Validate structure
    - Extract components

3. Dependency Resolution
    - Build dependency graph
    - Validate references
    - Determine load order

4. State Construction
    - Apply components in order
    - Validate final state
    - Report any issues

## 6. State Management

### 6.1 Operations

```yaml
Operations:
  - Create: First-time component creation
  - Update: Modify existing component
  - Delete: Remove component
```

### 6.2 Version Control

- Linear version history
- Immutable versions
- Atomic changes
- Full rollback support

### 6.3 State Transitions

- Each change creates new version
- Changes are atomic
- State is always consistent
- Failed changes are rolled back

## 7. Variable Interpolation

### 7.1 Syntax

```yaml
field: "${VAR}"                    # Required variable
field: "${VAR:-default}"           # Optional with default
field: "${VAR:?error message}"     # Required with error message
```

### 7.2 Rules

- Available in configuration values
- Not available in meta fields
- Not available in component references
- Environment variables take precedence

## 8. Validation Rules

### 8.1 Required Elements

- namespace - Component namespace
- name - Component identifier
- kind - Component type

### 8.2 Reference Validation

- Must be fully qualified
- Must exist at runtime
- Must not create cycles
- Must resolve to valid target

## 9. Best Practices

### 9.1 Organization

- Group related components
- Use consistent naming
- Keep configurations focused
- Document structure

### 9.2 Dependencies

- Minimize cross-namespace dependencies
- Use explicit dependencies
- Avoid deep dependency chains
- Document external dependencies

### 9.3 Security

- No secrets in files
- Use environment for sensitive data
- Validate all inputs
- Restrict access appropriately

### 9.4 Versioning

- Version your components
- Use semantic versioning
- Document changes
- Test transitions