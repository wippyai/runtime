# Module Dependencies and Requirements

This section explains how to use the requirements and dependencies 
system in our runtime engine configuration to manage dependencies between modules.

## Overview

The requirements and dependencies system provides a simple and powerful way to manage module dependencies. The system uses **direct parameter name matching** between `ns.requirement` and `ns.dependency` entries, making it much simpler and more intuitive than the previous approach.

## Key Components

### 1. Registry Entry Types

The system uses two registry entry types:

- **`ns.dependency`**: Represents a module dependency entry with configurable parameters
- **`ns.requirement`**: Represents a module requirement entry that specifies where dependency values should be injected

> **Note**: The `ns.definition` entry type has been **completely deprecated and removed** from the system.

### 2. Simplified Parameter Injection System

The system now uses a **direct parameter name matching** approach where:
- Dependencies declare parameters with names that directly match requirement entry names
- Requirements automatically find and use dependency parameters with matching names
- No complex path expressions or JSONPath syntax is required for basic parameter injection
- The system is much simpler and more predictable

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

The new system replaces the old `exports` section and the deprecated `ns.definition` approach with a more flexible and simpler approach using `ns.requirement` and `ns.dependency` entries:

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

### New Simplified Approach
```yaml
version: "1.0"
namespace: app.requirements.demo

meta:
  depends_on: ["ns:system"]
  comment: "Requirements and Dependencies Demo Application"

entries:
  # Dependencies - external components this demo needs
  - name: hello_world_dependency
    kind: "ns.dependency"
    meta:
      description: "Component dependency management demo example"
    component: "igor-test-3/test-2"
    version: ">=v0.0.1"
    namespace: "app.requirements.demo"
    parameters:
      - name: API_ROUTER
        value: "system:api"
      - name: NAMESPACE
        value: "ns:system"
```

## Simplified Parameter Injection Mechanism

The new system automatically handles parameter injection based on **direct name matching**:

- **Requirement names** (like `NAMESPACE`, `API_ROUTER`) automatically match **dependency parameter names**
- **No complex targeting syntax** is required for basic parameter injection
- **Automatic discovery** of matching parameters between requirements and dependencies
- **Simplified configuration** with fewer moving parts

## Implementation Details

### Core Components

1. **Registry API** (`api/registry/registry.go`):
   - Defines the entry kinds: `KindNamespaceDependency`, `KindNamespaceRequirement`
   - **Removed**: `KindNamespaceDefinition` (deprecated)
   - Provides the foundation for the new system

2. **Requirement Resolver** (`requirementresolver/resolver.go`):
   - Implements `Resolver` struct with `NewResolver()` constructor
   - Provides `ResolveModuleDefinitions()` method for parameter injection
   - **New**: Uses direct parameter name matching instead of complex path resolution
   - **Simplified**: Automatically finds dependency parameters that match requirement names
   - Provides extensive structured logging for debugging

3. **Module Loader** (`moduleloader/manager.go`):
   - `EntryLoader` implements `ManifestLoader` using registry entries
   - Converts `ns.dependency` entries to `ManifestDependency` format
   - **Enhanced**: Added module queue management and duplicate prevention
   - Handles component name parsing and validation

4. **Entry Loader** (`system/registry/loader/entry_loader.go`):
   - `ExtractDependenciesToEntries()` function processes YAML files
   - Converts requirements and dependencies to registry entries
   - Handles metadata merging between file-level and entry-level definitions

5. **Bus Runner** (`system/registry/runner/bus_runner.go`):
   - Processes the new entry kinds during registry operations
   - **Updated**: Removed support for deprecated `ns.definition` entries
   - Handles state transitions for requirements and dependencies

### Key Functions

#### `NewResolver(logger *zap.Logger) *Resolver`
Creates a new Resolver instance with the given logger.

#### `(r *Resolver) ResolveModuleDefinitions(entries []registry.Entry) error`
This function orchestrates the simplified parameter injection process:

1. **Builds maps** of `ns.dependency` and `ns.requirement` entries
2. **Automatically finds matching parameters** using direct name matching between requirements and dependency parameters
3. **Extracts values** from dependency parameters with matching names
4. **Applies values** to target entries using the specified paths
5. **Handles complex scenarios** like array appending and nested field access

#### `ExtractDependenciesToEntries(p payload.Payload, dtt payload.Transcoder) ([]registry.Entry, error)`
This function processes YAML configuration files:

1. **Unmarshals** YAML content into `FileContent` structure
2. **Processes requirements** and converts them to `ns.requirement` entries
3. **Processes raw entries** and applies metadata merging
4. **Validates** required fields and structure
5. **Returns** a slice of registry entries ready for processing

### Simplified Parameter Processing

The new system eliminates the need for complex parameter path processing in most cases:

#### Automatic Parameter Matching
- **Requirement name**: `NAMESPACE` automatically matches **dependency parameter**: `name: NAMESPACE`
- **Requirement name**: `API_ROUTER` automatically matches **dependency parameter**: `name: API_ROUTER`

#### Legacy Path Support (Backward Compatibility)
- Complex JSONPath expressions are still supported for advanced use cases
- `parameters[name=api_router].value` - Array filtering with field access
- `meta.depends_on[]` - Array appending

## Demo Application

A complete demo application is provided in `app/src/demos/requirements_demo/`:

### Files
- `_index.yaml`: Main configuration with simplified requirements and dependencies
- `demo_handler.lua`: Lua function demonstrating the parameter injection system
- `README.md`: Detailed documentation of the demo

### Demo Features
- **Simplified Configuration**: No more complex `ns.definition` entries
- **Direct Parameter Matching**: Automatic parameter injection based on name matching
- **Cleaner Syntax**: Reduced configuration complexity
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
1. **`ns.definition` Entries**: **Completely removed** - no longer supported
2. **Complex Path Expressions**: Replaced with direct parameter name matching
3. **Simplified Configuration**: Much cleaner and more intuitive
4. **Automatic Parameter Discovery**: System automatically finds matching parameters

### Migration Steps
1. **Remove all `ns.definition` entries** - they are no longer supported
2. **Simplify parameter targeting** - use direct name matching instead of complex paths
3. **Update dependency parameters** - ensure parameter names match requirement names exactly
4. **Test parameter injection** flow with the simplified system

## Benefits of New System

1. **Simplicity**: Much easier to understand and configure
2. **Reliability**: Fewer moving parts means fewer failure points
3. **Maintainability**: Cleaner code and configuration
4. **Performance**: Faster parameter resolution with direct name matching
5. **Debugging**: Easier to troubleshoot parameter injection issues
6. **Integration**: Direct integration with registry system

## Future Enhancements

The system is designed to be extensible for future enhancements:
- Additional parameter types
- More complex targeting scenarios (when needed)
- Dynamic parameter resolution
- Cross-namespace dependencies
- Parameter validation and constraints