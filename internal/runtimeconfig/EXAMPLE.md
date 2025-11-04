# Runtime Configuration - Examples

This document provides practical examples of using the runtime configuration feature.

## Command-Line Examples

### Basic Usage

Set a simple configuration value:

```bash
wippy -r app:gateway:addr=:8080 run
```

### Multiple Configuration Values

Set multiple configuration values:

```bash
wippy -r app:gateway:addr=:8080 -r app:gateway:port=8080 run
```

### Nested Fields

Set nested field values:

```bash
wippy \
  -r app:gateway:timeouts.read=30s \
  -r app:gateway:timeouts.write=30s \
  run
```

### Entry and Field with Dots

Since entry and field names can contain dots, colons are used as separators:

```bash
wippy -r app:agents_by_name.endpoint:meta.router=core:api run
```

### Complete Example

```bash
wippy \
  -r app:gateway:addr=:8080 \
  -r app:user:name=John \
  -r app:user:age=30 \
  -r app:db:host=localhost \
  run
```

### Database Configuration

Configure a complete database connection:

```bash
wippy \
  -r app:db:host=localhost \
  -r app:db:port=5432 \
  -r app:db:name=myapp \
  -r app:db:username=admin \
  -r app:db:password=secret \
  -r app:db:ssl.enabled=true \
  -r app:db:ssl.cert=/etc/certs/db.crt \
  run
```

### Nested Field Configuration

Configure nested fields:

```bash
wippy \
  -r app:gateway:timeouts.read=30s \
  -r app:gateway:timeouts.write=30s \
  -r app:gateway:timeouts.idle=60s \
  run
```

### Multiple Namespaces

Use different namespaces for different components:

```bash
wippy \
  -r app:name:value=MyApp \
  -r app:version:value=1.0.0 \
  -r config:debug:value=true \
  -r config:log:level=debug \
  -r service:api:endpoint=https://api.example.com \
  -r service:api:timeout=60 \
  run
```

### Special Characters in Values

Values can contain equals signs and other special characters:

```bash
wippy \
  -r app:db:connstr="host=localhost port=5432 dbname=myapp" \
  -r app:api:key="abc123==" \
  -r app:message:value="Hello, World!" \
  run
```

### Boolean and Numeric Values

Set values that will be parsed as different types:

```bash
wippy \
  -r app:features:auth=true \
  -r app:features:ssl=false \
  -r app:limits:connections=1000 \
  -r app:limits:rate=3.14 \
  run
```

## Programmatic Examples

### Accessing Configuration in Go Code

```go
package main

import (
    "fmt"
    "github.com/ponyruntime/pony/internal/runtimeconfig"
)

func main() {
    cfg := runtimeconfig.New()
    
    // Parse from command-line arguments
    cfg.SetFromString("app:gateway:addr=:8080")
    cfg.SetFromString("app:user:name=John")
    cfg.SetFromString("app:user:age=30")
    
    // Access as string
    if addr, exists, _ := cfg.GetString("app", "gateway", "addr"); exists {
        fmt.Printf("Gateway address: %s\n", addr)
    }
    
    // Access as string
    if age, exists, err := cfg.GetString("app", "user", "age"); exists && err == nil {
        fmt.Printf("User age: %s\n", age)
    }
    
    // Check if a key exists
    if cfg.Has("app", "user", "name") {
        name, _, _ := cfg.GetString("app", "user", "name")
        fmt.Printf("User name: %s\n", name)
    }
}
```

### Building Configuration from Multiple Sources

```go
package main

import (
    "fmt"
    "os"
    "github.com/ponyruntime/pony/internal/runtimeconfig"
)

func main() {
    cfg := runtimeconfig.New()
    
    // Load from environment variables
    if port := os.Getenv("APP_PORT"); port != "" {
        cfg.Set("app", "gateway", "addr", ":"+port)
    }
    
    // Load from command-line
    for _, arg := range os.Args {
        if len(arg) > 2 && arg[:2] == "-r" {
            // Parse runtime config
            cfg.SetFromString(arg[2:])
        }
    }
    
    // Provide defaults
    if !cfg.Has("app", "gateway", "addr") {
        cfg.Set("app", "gateway", "addr", ":8080")
    }
    
    // Use the configuration
    port, _, _ := cfg.GetString("app", "gateway", "addr")
    fmt.Printf("Starting server on %s\n", port)
}
```

### Type-Safe Configuration Access

```go
package main

import (
    "fmt"
    "github.com/ponyruntime/pony/internal/runtimeconfig"
)

type AppConfig struct {
    Gateway GatewayConfig
    User    UserConfig
    DB      DBConfig
}

type GatewayConfig struct {
    Addr string
}

type UserConfig struct {
    Name string
    Age  int
}

type DBConfig struct {
    Host string
}

func LoadConfig(cfg *runtimeconfig.Config) (*AppConfig, error) {
    appCfg := &AppConfig{}
    
    // Load gateway config
    if addr, exists, _ := cfg.GetString("app", "gateway", "addr"); exists {
        appCfg.Gateway.Addr = addr
    }
    
    // Load user config
    if name, exists, _ := cfg.GetString("app", "user", "name"); exists {
        appCfg.User.Name = name
    }
    if age, exists, err := cfg.GetString("app", "user", "age"); exists && err == nil {
        appCfg.User.Age = age
    }
    
    // Load DB config
    if host, exists, _ := cfg.GetString("app", "db", "host"); exists {
        appCfg.DB.Host = host
    }
    
    return appCfg, nil
}

func main() {
    cfg := runtimeconfig.New()
    cfg.SetFromString("app:gateway:addr=:8080")
    cfg.SetFromString("app:user:name=John")
    cfg.SetFromString("app:user:age=30")
    cfg.SetFromString("app:db:host=localhost")
    
    appCfg, err := LoadConfig(cfg)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Config: %+v\n", appCfg)
}
```

### Exporting Configuration

```go
package main

import (
    "encoding/json"
    "fmt"
    "github.com/ponyruntime/pony/internal/runtimeconfig"
)

func main() {
    cfg := runtimeconfig.New()
    cfg.SetFromString("app:gateway:addr=:8080")
    cfg.SetFromString("app:user:name=John")
    cfg.SetFromString("app:user:age=30")
    
    // Export to map
    allConfig := cfg.ToMap()
    
    // Convert to JSON
    jsonData, _ := json.MarshalIndent(allConfig, "", "  ")
    fmt.Printf("Configuration as JSON:\n%s\n", string(jsonData))
}
```

Output:
```json
{
  "app": {
    "db": {
      "host": "localhost"
    },
    "gateway": {
      "addr": "8080"
    },
    "user": {
      "age": "30",
      "name": "John"
    }
  }
}
```

## Error Handling Examples

### Invalid Format Detection

```go
package main

import (
    "fmt"
    "github.com/ponyruntime/pony/internal/runtimeconfig"
)

func main() {
    cfg := runtimeconfig.New()
    
    // Missing colon
    err := cfg.SetFromString("appgateway:addr=8080")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        // Output: Error: invalid format: missing ':' separator
    }
    
    // Missing equals
    err = cfg.SetFromString("app:gateway:addr")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        // Output: Error: invalid format: missing '=' separator
    }
    
    // Empty namespace
    err = cfg.SetFromString(":gateway:addr=8080")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        // Output: Error: invalid format: namespace cannot be empty
    }
}
```

### Path Conflict Detection

```go
package main

import (
    "fmt"
    "github.com/ponyruntime/pony/internal/runtimeconfig"
)

func main() {
    cfg := runtimeconfig.New()
    
    // First, set entry to a map structure
    cfg.Set("app", "gateway", "addr", "8080")
    
    // Try to set entry to a simple value (conflict)
    // This would fail because gateway already has nested fields
    err := cfg.Set("app", "gateway", "", "simple-value")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        // Output: Error: invalid field: cannot be empty
    }
    
    // Or set a conflicting nested path
    err = cfg.Set("app", "gateway", "addr.port", "9090")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        // Output: Error: path conflict: 'addr' is already set to a value
    }
}
```

### Type Conversion Errors

```go
package main

import (
    "fmt"
    "strconv"
    "github.com/ponyruntime/pony/internal/runtimeconfig"
)

func main() {
    cfg := runtimeconfig.New()
    cfg.Set("app", "gateway", "port", "not-a-number")
    
    // Get string value
    portStr, exists, err := cfg.GetString("app", "gateway", "port")
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
    if exists {
        // Try to parse as integer
        port, err := strconv.Atoi(portStr)
        if err != nil {
            fmt.Printf("Error: %v\n", err)
            // Output: Error: strconv.Atoi: parsing "not-a-number": invalid syntax
        } else {
            fmt.Printf("Port: %d\n", port)
        }
    }
    
    // The key exists even though conversion failed
    fmt.Printf("Key exists: %v\n", exists)
    // Output: Key exists: true
}
```


