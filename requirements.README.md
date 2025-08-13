# Module Dependencies and Requirements

This section explains how to use the requirements and dependencies 
system in our runtime engine configuration to manage dependencies between modules.

## Overview

The requirements and dependencies system provides a simple and powerful way to manage module dependencies. The system uses direct parameter name matching between `ns.requirement` and `ns.dependency` entries.

## Key Components

### 1. Registry Entry Types

The system uses two registry entry types:

- **`ns.dependency`**: Represents a module dependency entry with configurable parameters
- **`ns.requirement`**: Represents a module requirement entry that specifies where dependency values should be injected

### 2. Parameter Injection System

The system uses a simple parameter-based approach where:
- Dependencies declare parameters with names that directly match requirement entry names
- Requirements specify target locations where dependency parameter values should be injected
- The system uses direct name matching instead of complex path expressions

## External Module Declaration

Each external module has a YAML file with declaration containing two sections: "requirements" and "entries":

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
        - entry: hello_endpoint
          path: meta.router

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

Each requirement item declares public parameters that might be modified when the module is used by other code as a dependency:

```yaml
  - parameter: NAMESPACE
    description: "Target namespace for module dependency"
    targets:
      - path: meta.depends_on[]
```

## New Application Requirements System

The new system replaces the old `exports` section with a more flexible approach using `ns.requirement` and `ns.dependency` entries:

### Old Approach (Deprecated)
```yaml
exports:
  - name: NAMESPACE
    description: "System namespace reference"
    value: "ns:system"

  - name: API_ROUTER
    description: "Main API router for endpoints"
    value: "system:api"
```

### New Approach
```yaml
version: "1.0"
namespace: app.requirements.demo

meta:
  depends_on: [ "ns:system" ]
  comment: "Requirements and Dependencies Demo Application"

entries:
  # Requirements - what this demo needs from the system
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

  # Dependency
  - name: hello_world_dependency
    kind: "ns.dependency"
    meta:
      description: "Component dependency management demo example"
    component: "igor-test-3/test-2"
    version: ">=v0.0.1"
    namespace: "app.requirements.demo"
    parameters:
      - name: api_router
        value: "system:api"
      - name: namespace
        value: "ns:system"
```

## Parameter Injection Mechanism

The declaration below means that we need to extract the value of `hello_world_dependency.namespace` and pass it as parameter `NAMESPACE` to the `hello_handler` dependency:

```yaml
  - name: NAMESPACE
    kind: ns.requirement
    targets:
      - entry: hello_world_dependency
        path: namespace
```

## JSONPath-Like Targeting Syntax

The system uses JSONPath-like syntax for precise parameter targeting:

### Targeting Dependency Parameters
- `parameters[name=namespace].value` - Targets the `value` field of the parameter named `namespace`
- `parameters[name=api_router].value` - Targets the `value` field of the parameter named `api_router`

### Targeting Module Configuration
- `meta.depends_on[]` - Targets the `depends_on` array in the module's meta section
- `meta.router` - Targets the `router` field in the module's meta section

## Implementation Details

### Core Components

1. **Registry API** (`api/registry/registry.go`):
   - Defines the new entry kinds: `KindNamespaceDefinition`, `KindNamespaceDependency`, `KindNamespaceRequirement`
   - Provides the foundation for the new system

2. **Requirement Resolver** (`requirementresolver/resolver.go`):
   - Implements `Resolver` struct with `NewResolver()` constructor
   - Provides `ResolveModuleRequirements()` method for parameter injection
   - Handles parameter injection using JSONPath-like syntax
   - Supports complex parameter targeting with array filters
   - Provides extensive structured logging for debugging

3. **Module Loader** (`moduleloader/registry_loader.go`):
   - `EntryLoader` implements `ManifestLoader` using registry entries
   - Converts `ns.dependency` entries to `ManifestDependency` format
   - Handles component name parsing and validation

4. **Entry Loader** (`system/registry/loader/entry_loader.go`):
   - `ExtractDependenciesToEntries()` function processes YAML files
   - Converts requirements and dependencies to registry entries
   - Handles metadata merging between file-level and entry-level definitions

5. **Bus Runner** (`system/registry/runner/bus_runner.go`):
   - Processes the new entry kinds during registry operations
   - Handles state transitions for requirements and dependencies

### Key Functions

#### `NewResolver(logger *zap.Logger) *Resolver`
Creates a new Resolver instance with the given logger.

#### `(r *Resolver) ResolveModuleRequirements(entries []registry.Entry) error`
This function orchestrates the parameter injection process:

1. **Builds maps** of `ns.definition`, `ns.dependency`, and `ns.requirement` entries
2. **Finds requirement-dependency relationships** using target specifications
3. **Extracts values** from dependency entries using JSONPath-like paths
4. **Applies values** to target entries using the specified paths
5. **Handles complex scenarios** like array appending and nested field access

#### `ExtractDependenciesToEntries(p payload.Payload, dtt payload.Transcoder) ([]registry.Entry, error)`
This function processes YAML configuration files:

1. **Unmarshals** YAML content into `FileContent` structure
2. **Processes requirements** and converts them to `ns.definition` entries
3. **Processes raw entries** and applies metadata merging
4. **Validates** required fields and structure
5. **Returns** a slice of registry entries ready for processing

### Parameter Path Processing

The system supports sophisticated parameter path processing:

#### Simple Paths
- `namespace` - Direct field access
- `meta.description` - Nested field access

#### Array Targeting
- `parameters[name=api_router].value` - Array filtering with field access
- `meta.depends_on[]` - Array appending

#### Complex Scenarios
- `parameters[name=text].value` - Parameter lookup with value extraction
- `config.auth.key` - Deep nested configuration access

## Demo Application

A complete demo application is provided in `app/src/demos/requirements_demo/`:

### Files
- `_index.yaml`: Main configuration with requirements and dependencies
- `demo_handler.lua`: Lua function demonstrating the parameter injection system
- `README.md`: Detailed documentation of the demo

### Demo Features
- **Multiple Requirements**: Demonstrates different types of requirements
- **Parameter Injection**: Shows how values flow from requirements to dependencies
- **JSONPath Targeting**: Examples of complex parameter targeting
- **HTTP Integration**: Endpoint that displays the injection flow
- **Lua Integration**: Handler function that accesses and displays the system

### Testing the Demo
Access the demo endpoint at `GET /api/v1/demo/requirements` to see:
- Resolved requirements with target information
- Dependency information including parameters
- Parameter injection flow demonstration
- Metadata about the demo structure

## Migration from Old System

### What Changed
1. **Exports Section**: Removed in favor of `ns.requirement` entries
2. **Parameter Injection**: New JSONPath-like targeting system
3. **Dependency Management**: More flexible parameter-based approach
4. **Registry Integration**: Direct integration with registry system

### Migration Steps
1. **Replace exports** with `ns.requirement` entries
2. **Add `ns.dependency`** entries for external components
3. **Update parameter targeting** using JSONPath-like syntax
4. **Test parameter injection** flow

## Benefits of New System

1. **Flexibility**: More granular control over parameter injection
2. **Precision**: JSONPath-like syntax for exact targeting
3. **Extensibility**: Easy to add new parameter types and targeting methods
4. **Debugging**: Extensive logging for troubleshooting
5. **Integration**: Direct integration with registry system
6. **Validation**: Built-in validation and error handling

## Future Enhancements

The system is designed to be extensible for future enhancements:
- Additional parameter types
- More complex targeting scenarios
- Dynamic parameter resolution
- Cross-namespace dependencies
- Parameter validation and constraints