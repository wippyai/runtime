# Requirements and Dependencies Demo

This demo application showcases the new `ns.requirement` and `ns.dependency` system in the Pony Runtime with parameter injection capabilities.

## Overview

The demo demonstrates how to:
- Declare requirements using `ns.requirement` entries
- Define dependencies using `ns.dependency` entries with parameters
- Inject requirement values into specific dependency parameters via target paths
- Use parameter paths with JSONPath-like syntax for precise targeting
- Create HTTP endpoints that demonstrate the parameter injection flow

## How the Requirements System Works

### 1. Module Requirements Declaration

The module component (`module_example.yaml`) declares what parameters it needs through a `requirements` section:

```yaml
requirements:
  - parameter: NAMESPACE
    description: "Target namespace for module dependency"
    targets:
      - value: meta.depends_on[]

  - parameter: API_ROUTER
    description: "Router to use for endpoints"
    targets:
        - name: hello_endpoint
          value: meta.router
```

This tells the system that the module expects two parameters:
- `NAMESPACE`: Will be injected into `meta.depends_on[]` 
- `API_ROUTER`: Will be injected into the `hello_endpoint` entry's `meta.router` field

### 2. Application Requirements (`ns.requirement`)

The application (`_index.yaml`) declares requirements that provide values for the module's expected parameters:

```yaml
- name: NAMESPACE
  kind: ns.requirement
  targets:
    - entry: hello_world_dependency
      path: "parameters[name=namespace].value"

- name: API_ROUTER
  kind: ns.requirement
  targets:
    - entry: hello_world_dependency
      path: "parameters[name=api_router].value"
```

These requirements:
- Declare what values the application needs
- Specify which dependency entry to inject into (`hello_world_dependency`)
- Use JSONPath-like syntax to target specific parameters within the dependency

### 3. Dependency Configuration (`ns.dependency`)

The dependency entry defines the external component with its parameters:

```yaml
- name: hello_world_dependency
  kind: ns.dependency
  meta:
    description: "Component dependency management demo example"
  component: "igor-test-3/test-2"
  version: ">=v0.0.1"
  parameters:
    - name: namespace
      value: "app.requirements.demo"
    - name: api_router
      value: "system:api"
```

The parameters section provides default values that can be overridden by requirements.

## Parameter Injection Flow

1. **Module Declaration**: The module declares what parameters it needs in its `requirements` section
2. **Application Requirements**: The application declares `ns.requirement` entries that provide values
3. **Target Specification**: Requirements specify which dependency and parameter to inject into using JSONPath syntax
4. **Value Injection**: The system injects requirement values into dependency parameters
5. **Module Usage**: The module receives the injected values and uses them in its configuration

## JSONPath Targeting Syntax

The system uses JSONPath-like syntax for precise parameter targeting:

### Targeting Dependency Parameters
- `parameters[name=namespace].value` - Targets the `value` field of the parameter named `namespace`
- `parameters[name=api_router].value` - Targets the `value` field of the parameter named `api_router`

### Targeting Module Configuration
- `meta.depends_on[]` - Targets the `depends_on` array in the module's meta section
- `meta.router` - Targets the `router` field in the module's meta section

## Complete Flow Example

1. **Module declares requirements**:
   ```yaml
   requirements:
     - parameter: NAMESPACE
       targets:
         - value: meta.depends_on[]
   ```

2. **Application provides requirement**:
   ```yaml
   - name: NAMESPACE
     kind: ns.requirement
     targets:
       - entry: hello_world_dependency
         path: "parameters[name=namespace].value"
   ```

3. **Dependency defines parameter**:
   ```yaml
   parameters:
     - name: namespace
       value: "app.requirements.demo"
   ```

4. **System injects value**: The requirement value gets injected into the dependency parameter, which then flows to the module's `meta.depends_on[]` field.

## Key Features Demonstrated

- **Module Requirements**: Modules declare what parameters they need
- **Parameter-Based Requirements**: Requirements that target specific dependency parameters
- **JSONPath-Like Targeting**: Precise parameter targeting using path syntax
- **Parameter Injection Flow**: Clear demonstration of how values flow from requirements to dependencies to modules
- **Multiple Parameters**: Dependencies with multiple configurable parameters
- **HTTP Endpoint Integration**: Endpoints that demonstrate the parameter injection system
- **Lua Function Integration**: Handler functions that can access and display the injection flow

## Testing the Demo

Once deployed, you can access the demo endpoint at:
```
GET /api/v1/demo/requirements
```

The endpoint will return a JSON response showing:
- All resolved requirements with their target information
- Dependency information including parameters
- Parameter injection flow demonstration
- Metadata about the demo structure

## Files

- `_index.yaml`: Main configuration file with requirements and dependencies using parameter injection
- `demo_handler.lua`: Lua function that demonstrates the parameter injection system
- `README.md`: This documentation file 