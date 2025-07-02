# Requirements and Dependencies Demo

This demo showcases the `ns.requirement` and `ns.dependency` system in the Pony Runtime with parameter injection capabilities.

## Overview

The demo demonstrates:
- Declaring requirements using `ns.requirement` entries
- Defining dependencies using `ns.dependency` entries with parameters
- Injecting requirement values into dependency parameters via target paths
- Using JSONPath-like syntax for precise parameter targeting

## How It Works

### 1. Application Requirements (`ns.requirement`)

The application declares requirements that provide values for dependency parameters:

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

### 2. Dependency Configuration (`ns.dependency`)

Dependencies define external components with their parameters:

```yaml
- name: hello_world_dependency
  kind: "ns.dependency"
  meta:
    description: "Component dependency management demo example"
  component: "igor-test-3/test-2"
  version: ">=v0.0.1"
  parameters:
    - name: api_router
      value: "system:api"
    - name: namespace
      value: "ns:system"
```

### 3. Module Requirements Declaration

The module (`module_example.yaml`) declares what parameters it needs through a `requirements` section:

```yaml
version: "1.0.0"
namespace: localspace

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

entries:
  - name: hello_handler
    kind: function.lua
    source: file://hello_world.lua
    method: handler
    meta:
      comment: "Lua function for hello_endpoint"
    modules: [ http ]
    pool:
      size: 2
      workers: 4

  - name: hello_endpoint
    kind: http.endpoint
    method: GET
    meta:
      comment: "HTTP endpoint which executes hello_handler"
    path: "/local/hello"
    func: hello_handler
```

### 4. Complete Application Configuration

Here's the complete `_index.yaml` from the demo:

```yaml
version: "1.0"
namespace: app.requirements.demo

meta:
  depends_on: [ "ns:system" ]
  comment: "Requirements and Dependencies Demo Application"

entries:
  - name: NAMESPACE
    kind: ns.requirement
    targets:
      - entry: hello_world_dependency
        path: parameters[name=namespace].value

  - name: API_ROUTER
    kind: ns.requirement
    targets:
      - entry: hello_world_dependency
        path: parameters[name=api_router].value

  - name: hello_world_dependency
    kind: "ns.dependency"
    meta:
      description: "Component dependency management demo example"
    component: "igor-test-3/test-2"
    version: ">=v0.0.1"
    parameters:
      - name: api_router
        value: "system:api"
      - name: namespace
        value: "ns:system"
```

## Parameter Injection Flow

1. **Application Requirements**: Application declares `ns.requirement` entries with path where the value is located
2. **Target Specification**: Requirements specify where to locate value for dependency
3. **Value Injection**: System injects requirement values into target entries

## JSONPath Targeting Syntax

The system uses JSONPath-like syntax for parameter targeting. In this demo, we focus on targeting dependency parameters:

- `parameters[name=namespace].value` - Locates the `parameters` slice, finds the section with `name=namespace`, then takes the `value` field
- `parameters[name=api_router].value` - Locates the `parameters` slice, finds the section with `name=api_router`, then takes the `value` field

## Key Features

- **Module Requirements**: Modules declare what parameters they need
- **Parameter-Based Requirements**: Requirements target specific dependency parameters
- **JSONPath-Like Targeting**: Precise parameter targeting using path syntax
- **Parameter Injection Flow**: Clear demonstration of value flow from requirements to dependencies to modules
- **HTTP Endpoint Integration**: Endpoints that demonstrate the parameter injection system

## Testing the Demo

Access the demo endpoint at:
```
GET /api/v1/local/hello
```

The endpoint demonstrates the requirements and dependencies system in action, showing how:
- Requirements are resolved and injected into dependencies
- Parameter values flow from application requirements to module configuration
- The system connects modules, dependencies, and requirements together

## Files

- `_index.yaml`: Main configuration with requirements and dependencies
- `README.md`: This documentation 