# Requirements and Dependencies Demo

This demo showcases the `ns.requirement`, `ns.dependency`, and `ns.definition` system in the Pony Runtime with parameter injection capabilities.

## Overview

The demo demonstrates:
- Declaring requirements using `ns.requirement` entries in the application
- Defining dependencies using `ns.dependency` entries with parameters
- Creating definitions using `ns.definition` entries in modules to specify where values should be injected
- Injecting requirement values into target entries via jq syntax paths
- Complete end-to-end flow from application requirements to module definitions

## How It Works

### 1. Application Requirements (`_index.yaml`)

The application declares requirements that provide values for dependency parameters:

```yaml
- name: NAMESPACE
  kind: ns.requirement
  targets:
    - entry: hello_world_dependency
      path: ".parameters[] | select(.name == \"namespace\") | .value"

- name: API_ROUTER
  kind: ns.requirement
  targets:
    - entry: hello_world_dependency
      path: ".parameters[] | select(.name == \"api_router\") | .value"
```

### 2. Dependency Configuration (`_index.yaml`)

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

### 3. Module Definitions (`module_example.yaml`)

The module declares definitions that specify where the resolved requirement values should be injected:

```yaml
- name: NAMESPACE
  kind: ns.definition
  meta:
    description: "Target namespace for module dependency"
  targets:
    - value: ".meta.depends_on +="

- name: API_ROUTER
  kind: ns.definition
  meta:
    description: "Router to use for endpoints"
  targets:
    - name: hello_endpoint
      value: ".meta.router"
```

### 4. Module Target Entries (`module_example.yaml`)

The module entries that will receive the injected values:

```yaml
- name: hello_endpoint
  kind: http.endpoint
  method: GET
  meta:
    comment: "HTTP endpoint which executes hello_handler"
  path: "/local/hello"
  func: hello_handler

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
```

### 5. Complete Application Configuration

**Application (`_index.yaml`):**
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
        path: .parameters[] | select(.name == "namespace") | .value

  - name: API_ROUTER
    kind: ns.requirement
    targets:
      - entry: hello_world_dependency
        path: .parameters[] | select(.name == "api_router") | .value

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

**Module (`module_example.yaml`):**
```yaml
version: "1.0.0"
namespace: localspace

entries:
  - name: NAMESPACE
    kind: ns.definition
    meta:
      description: "Target namespace for module dependency"
    targets:
      - value: ".meta.depends_on +="

  - name: API_ROUTER
    kind: ns.definition
    meta:
      description: "Router to use for endpoints"
    targets:
      - name: hello_endpoint
        value: ".meta.router"

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

## Parameter Injection Flow

1. **Application Requirements**: Application declares `ns.requirement` entries with jq path where the value is located in dependencies
2. **Dependency Resolution**: System finds the dependency entry and extracts the value using the specified jq path
3. **Module Definition Lookup**: System finds the corresponding `ns.definition` entry in the module for each requirement
4. **Target Injection**: System injects the resolved values into the target entries specified in the module definitions
5. **Value Application**: Module entries receive the injected values in their metadata or other fields

## jq Targeting Syntax

The system uses jq syntax for parameter targeting. In this demo, we focus on targeting dependency parameters:

- `.parameters[] | select(.name == "namespace") | .value` - Locates the `parameters` array, finds the object with `name == "namespace"`, then takes the `value` field
- `.parameters[] | select(.name == "api_router") | .value` - Locates the `parameters` array, finds the object with `name == "api_router"`, then takes the `value` field

For module definition targets, we use simpler paths:
- `.meta.depends_on +=` - Appends the value to the `depends_on` array in the target entry's metadata
- `.meta.router` - Sets the `router` field in the target entry's metadata

## Key Features

- **Separation of Concerns**: Application requirements and dependencies are separate from module definitions and targets
- **Parameter-Based Requirements**: Requirements target specific dependency parameters using jq syntax
- **Module Definition Targeting**: Module definitions specify exactly where values should be injected
- **jq Targeting**: Precise parameter targeting using jq syntax for both requirements and definitions
- **HTTP Endpoint Integration**: Endpoints that demonstrate the parameter injection system
- **Lua Function Integration**: Lua functions that can use the injected parameters

## Testing the Demo

Access the demo endpoint at:
```
GET /api/v1/local/hello
```

The endpoint demonstrates the requirements and dependencies system in action, showing how:
- Requirements are resolved and values extracted from dependencies
- Values are injected into module entries via definitions
- The complete flow connects application requirements, dependencies, and module definitions

## Files

- `_index.yaml`: Application configuration with requirements and dependencies
- `module_example.yaml`: Module configuration with definitions and target entries
- `README.md`: This documentation 