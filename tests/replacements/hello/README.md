# module-hello
Minimal hello module for wippy.ai platform

## Overview
This is a minimal example module for the wippy.ai platform that demonstrates basic module structure and functionality.

## Module Structure

The module is defined in `module_example.yaml` and follows the wippy.ai module specification:

### Version and Namespace
- `version`: Module version (e.g., "1.0.0")
- `namespace`: Module namespace (e.g., "localspace")

### Entries

The module contains several types of entries:

#### Namespace Requirements (`ns.requirement`)
Used to define requirements for the module:

```yaml
- name: NAMESPACE
  kind: ns.requirement
  meta:
    description: "Target namespace for module requirement"
  targets:
    - entry: ".meta.depends_on +="
```

- `kind: ns.requirement` - Specifies this is a namespace requirement
- `targets` - List of target configurations
  - `entry` - The identifier of target entry
  - `path` - The path where the requirement should be applied

#### Function Definitions (`function.lua`)
Defines Lua functions that can be executed:

```yaml
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

#### HTTP Endpoints (`http.endpoint`)
Defines HTTP endpoints that can be accessed:

```yaml
- name: hello_endpoint
  kind: http.endpoint
  method: GET
  meta:
    comment: "HTTP endpoint which executes hello_handler"
  path: "/local/hello"
  func: hello_handler
```

## Field Naming Convention

The module uses the following field naming convention:
- `entry` - Used for target identifiers
- `path` - Used for configuration paths

## Usage

1. Deploy the module to your wippy.ai environment
2. The module will create a GET endpoint at `/local/hello`
3. The endpoint will execute the `hello_handler` function defined in `hello_world.lua`

## Files

- `module_example.yaml` - Module configuration
- `hello_world.lua` - Lua function implementation
- `README.md` - This documentation
