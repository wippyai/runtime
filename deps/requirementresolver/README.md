# Default Values for Module Requirements

This document demonstrates the new default value functionality for module requirements in the Wippy system.

## Overview

The default value feature allows module requirements to define fallback values when application dependencies don't provide the required parameters. This improves usability and resilience of the modular system.

**Key Benefit**: When a module requirement has a `default` value, the `parameters` section in `ns.dependency` becomes **optional** in the application configuration.

## How It Works

### Before (Current Behavior)
- Application **must** pass parameters to dependencies
- Module requirements consume those parameters
- If no parameter is provided, the requirement fails

### After (New Behavior)
- Module requirements can declare default values
- If application doesn't provide a parameter, the system falls back to the default
- If application provides a parameter, it overrides the default
- **The `parameters` section in `ns.dependency` is now optional when defaults are defined**

## Example Usage

### Application Configuration (Simplified - Parameters Optional)
```yaml
# Application (_index.yaml) - Parameters section is now OPTIONAL
version: "1.0"
namespace: app.example

entries:
  - name: __dependency.wippy.llm
    kind: "ns.dependency"
    meta: 
      description: "LLM component"
    component: "wippy/llm"
    version: ">=v0.0.7"
    # parameters section is OPTIONAL when module has defaults
    # parameters: 
    #   - name: "application_host"
    #     value: "system:processes"
```

### Application Configuration (With Custom Parameters)
```yaml
# Application (_index.yaml) - Override defaults with custom values
version: "1.0"
namespace: app.example

entries:
  - name: __dependency.wippy.llm
    kind: "ns.dependency"
    meta: 
      description: "LLM component"
    component: "wippy/llm"
    version: ">=v0.0.7"
    parameters: 
      - name: "application_host"
        value: "system:processes"  # Overrides module default
```

### Module Configuration (With Default Values)
```yaml
# Module (wippy/llm/_index.yaml)
version: "1.0"
namespace: wippy.llm

entries: 
  - name: application_host
    kind: ns.requirement
    meta: 
      description: "Host ID for the application processes"
    targets: 
      - entry: token_refresh
        path: .meta.default_host
      - entry: token_refresh.service
        path: .host
      - entry: token_refresh.service
        path: ".lifecycle.depends_on +="
    default: "app:processes"  # <-- NEW: Default value (makes parameters optional)
```

## Behavior Examples

### Case 1: Application Provides Parameter
- **Application parameter**: `application_host = "system:processes"`
- **Module default**: `application_host = "app:processes"`
- **Result**: Uses `"system:processes"` (application value overrides default)

### Case 2: Application Doesn't Provide Parameter
- **Application parameter**: Not provided
- **Module default**: `application_host = "app:processes"`
- **Result**: Uses `"app:processes"` (default value is used)

### Case 3: Application Has No Parameters Field
- **Application dependency**: No `parameters` field in dependency entry
- **Module default**: `application_host = "app:processes"`
- **Result**: Uses `"app:processes"` (default value is used)

### Case 4: Application Has Nil Parameters Field
- **Application dependency**: `parameters: null` in dependency entry
- **Module default**: `application_host = "app:processes"`
- **Result**: Uses `"app:processes"` (default value is used)

### Case 5: Application Has Malformed Parameters Field
- **Application dependency**: `parameters: "not_an_array"` in dependency entry
- **Module default**: `application_host = "app:processes"`
- **Result**: Uses `"app:processes"` (default value is used)

### Case 6: No Parameter and No Default
- **Application parameter**: Not provided
- **Module default**: Not defined
- **Result**: Requirement is skipped (graceful degradation)

## Validation Rules

1. **Parameter Matching**: If `parameters.name` in application doesn't match any requirement name in module → warning logged
2. **Missing Values**: If `ns.requirement` has no default and application doesn't provide value → requirement is skipped (no fatal error)
3. **Default Values**: Module requirements can define default values as strings
4. **Optional Parameters**: When a module requirement has a `default` value, the `parameters` section in `ns.dependency` becomes **optional**

## Implementation Details

The feature is implemented in the `requirementresolver` package:

- `getRequirementDefaultValue()`: Extracts default values from requirement entries
- `validateParameterMatching()`: Validates parameter-requirement matching
- Enhanced `ResolveModuleDefinitions()`: Handles default value fallback logic
- Enhanced `findDependencyByParameterName()`: Robustly handles missing, nil, or malformed parameters fields

## Testing

Comprehensive tests cover:
- Requirements with default values (no dependency parameter)
- Requirements with default values (dependency parameter provided)
- Requirements without default values (no dependency parameter)
- Requirements with default values (dependency has no parameters field)
- Requirements with default values (dependency has nil parameters field)
- Requirements with default values (dependency has malformed parameters field)
- Default value extraction
- Parameter matching validation
- Edge cases for parameter field handling

All tests pass and maintain backward compatibility with existing functionality.

## Key Benefits

### 1. **Simplified Application Configuration**
When modules define default values, applications can use modules without specifying any parameters:

```yaml
# Minimal application configuration
entries:
  - name: __dependency.wippy.llm
    kind: "ns.dependency"
    component: "wippy/llm"
    version: ">=v0.0.7"
    # No parameters section needed!
```

### 2. **Flexible Override Capability**
Applications can still override defaults when needed:

```yaml
# Custom configuration when needed
entries:
  - name: __dependency.wippy.llm
    kind: "ns.dependency"
    component: "wippy/llm"
    version: ">=v0.0.7"
    parameters: 
      - name: "application_host"
        value: "custom:host"  # Override default
```

### 3. **Backward Compatibility**
Existing applications with parameters continue to work unchanged.

### 4. **Improved Developer Experience**
- **Less boilerplate**: No need to specify parameters for every module
- **Sensible defaults**: Modules can provide reasonable default values
- **Easy customization**: Override only what you need to change
