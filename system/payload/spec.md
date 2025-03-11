# Transcoder Component Overview

The Transcoder component is the central payload conversion system in the application. It enables seamless conversion
between different payload formats—such as JSON, YAML, Lua, and Golang native types—by using a pluggable and graph-based
approach. This component allows developers to register custom transcoding and unmarshaling functions so that data can be
transformed along the shortest available path when a direct conversion is not provided.

## Key Responsibilities

- **Registration of Transcoders:**  
  Developers can register functions that convert from one format to another by specifying source and target formats
  along with a weight (priority). These functions are stored in a conversion graph, which is used to determine the
  optimal path for multi-step conversions. citeturn2file5

- **Graph-Based Conversion Pathfinding:**  
  A directed graph models the relationships between formats. When converting a payload, the Transcoder calculates the
  shortest conversion path through this graph, ensuring efficient multi-step conversions when a direct transcoder is
  unavailable. citeturn2file5

- **Global Instance Management:**  
  A singleton global transcoder instance is provided via `GlobalTranscoder()`, ensuring that all components of the
  system share a consistent registry and conversion graph.

- **Unmarshaling Support:**  
  In addition to transcoding, the component supports unmarshaling: converting a payload (e.g. JSON string) into
  structured Go data. Unmarshallers can be registered for specific formats, and if a payload isn’t directly
  unmarshallable, the system will transcode it to a compatible format first. citeturn2file5

## Component Structure

### Registration and Graph Construction

- **RegisterTranscoder:**  
  Modules like JSON, YAML, and Lua register their conversion functions with the Transcoder. This involves adding nodes (
  representing formats) and weighted edges (representing conversions) to the graph. For instance, the JSON module
  registers a transcoder that converts JSON to Golang types. citeturn2file0

- **RegisterUnmarshaler:**  
  Unmarshallers are similarly registered. When a payload needs to be unmarshaled into a Go structure, the Transcoder
  checks if a direct unmarshaler exists; otherwise, it finds a conversion path to a format that does. citeturn2file5

### Conversion Process

1. **Path Computation:**  
   When converting a payload, the Transcoder first checks if the payload is already in the target format. If not, it
   computes the shortest path from the current format to the target using the conversion graph. Cached paths optimize
   repeated conversions.

2. **Iterative Conversion:**  
   The payload is transformed step by step along the computed path. Each step applies a registered transcoder, updating
   the payload’s format until the target format is reached. citeturn2file5

3. **Error Handling:**  
   If no conversion path exists or if any transcoding step fails, the system returns an error indicating the failure.

### Unmarshaling Workflow

- The `Unmarshal` method leverages registered unmarshallers to convert payloads into Go data structures. If a payload’s
  format isn’t directly supported for unmarshaling, the Transcoder computes a path to a format that is, performs the
  conversion, and then applies the unmarshaler. This makes it easy to work with various data representations uniformly.

## Supported Formats and Modules

- **JSON Module:**  
  Provides conversion between JSON (as a string or bytes) and Golang native types. It also implements unmarshaling for
  JSON payloads. citeturn2file0

- **YAML Module:**  
  Uses the `gopkg.in/yaml.v3` library to convert YAML payloads to and from Golang types. citeturn2file4

- **Lua Module:**  
  Supports conversions between Lua (using the gopher-lua package) and Golang payloads, and also registers transcoding
  between JSON and Lua formats. citeturn2file1, citeturn2file2, citeturn2file3

## Global Usage Example

Initialization might look like this:

```go
// Register format modules during startup.
json.Register(GlobalTranscoder())
yaml.Register(GlobalTranscoder())
lua.Register(GlobalTranscoder())

// Now, converting a payload from JSON to YAML:
payload, err := GlobalTranscoder().Transcode(inputPayload, payload.YAML)
if err != nil {
// handle error
}
```

Unmarshaling a payload into a Go struct follows a similar process:

```go
var result MyStruct
err := GlobalTranscoder().Unmarshal(jsonPayload, &result)
if err != nil {
// handle error
}
```

## Testing and Validation

Comprehensive tests verify that:

- Multi-step transcoding (e.g., JSON → Golang → YAML) works correctly.
- Error conditions, such as missing conversion paths, are properly handled.
- Unmarshaling across formats yields expected Go data structures.