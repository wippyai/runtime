# Lua Modules Index and System Overview

## System Rules and Restrictions

### Core Lua Libraries

- **Available Core Libraries:**
    - `base`: Basic Lua functions
    - `table`: Table manipulation
    - `string`: String manipulation
    - `math`: Mathematical functions
    - `debug`: Debugging facilities
    - `coroutine`: Coroutine functionality
    - `package`: Package/module loading (restricted)

- **Forbidden Core Libraries:**
    - `io`: File I/O operations (not available)
    - `os`: Operating system facilities (not available)

### Module Loading

- Modules are loaded using `require()`
- Some modules are automatically preloaded
- Package loading is restricted via custom loader

## Available Modules

### Time and Date

#### `time`

- Time-related functionality and time zone support
- Duration handling and formatting
- Timer and ticker creation
- Key features:
    - Time parsing and formatting
    - Duration calculations
    - Time zone conversions
    - Timer/ticker support for concurrent programming

### File System

#### `fs`

- Universal filesystem abstraction layer
- File and directory operations
- Key features:
    - File reading/writing
    - Directory operations
    - Path manipulation
    - Multiple backend support (local, S3, etc.)
    - Streaming support for large files

### HTTP and Networking

#### `http_client`

- HTTP client functionality
- Support for all standard HTTP methods
- Key features:
    - Request/response handling
    - Header/cookie management
    - Concurrent requests
    - Streaming responses

#### `websocket`

- WebSocket client implementation
- Integrates with Pony coroutine VM
- Key features:
    - Connection management
    - Message sending/receiving
    - Automatic ping handling
    - Event-based communication

### Data Formats

#### `json`

- JSON encoding/decoding
- Support for all basic Lua types
- Key features:
    - Table to JSON conversion
    - JSON string parsing
    - Error handling for invalid inputs

#### `base64`

- Base64 encoding/decoding
- Simple string conversion
- Key features:
    - String encoding to Base64
    - Base64 string decoding
    - Error handling for invalid inputs

### Cryptography

#### `hash`

- Cryptographic and non-cryptographic hashing
- Multiple hash algorithm support
- Key features:
    - MD5, SHA-1, SHA-256, SHA-512
    - FNV-1 32-bit and 64-bit
    - String input handling

### System Integration

#### `env`

- Environment variable access
- Safe and controlled environment interaction
- Key features:
    - Single variable retrieval
    - All variables retrieval
    - Error handling for missing variables

#### `ctx`

- Context management system
- Shared context interaction
- Key features:
    - Value getting/setting
    - Error handling
    - Thread-safe operations

### Logging

#### `logger`

- Structured logging interface
- Multiple log levels
- Key features:
    - Debug, info, warn, error levels
    - Structured field support
    - Logger creation with predefined fields

### UUID

#### `uuid`

- UUID generation and manipulation
- Multiple UUID version support
- Key features:
    - v1, v3, v4, v5, v7 UUID generation
    - UUID validation and parsing
    - Format conversion

### Web Server Integration

#### `http`

- HTTP request/response handling
- Server-side functionality
- Key features:
    - Request parsing
    - Response writing
    - Header manipulation
    - Server-sent events

### Tree-sitter Integration

#### `treesitter`

- Syntax tree parsing and analysis
- Language support
- Key features:
    - Multiple language support
    - Query functionality
    - Error handling
    - Resource management

### Data Upload

#### `upstream`

- Value sending to parent runtime
- Non-blocking channel communication
- Key features:
    - Value type handling
    - Non-blocking operations
    - Thread-safe implementation

## Best Practices

### Error Handling

- Most functions return two values: result and error
- Always check error returns
- Use pcall for potentially failing operations

### Resource Management

- Close files and streams after use
- Release resources explicitly when provided
- Use appropriate timeout values

### Threading

- Most modules are thread-safe
- Avoid sharing mutable state between threads
- Use appropriate synchronization mechanisms

### Performance

- Use streaming for large data
- Batch operations when possible
- Cache frequently used values