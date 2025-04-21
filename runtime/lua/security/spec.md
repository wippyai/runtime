# Lua Security Permissions Specification

## Overview

This document specifies the security permissions system implemented across Lua modules in the Pony runtime. Every sensitive operation requires an explicit permission check using the security API.

## Permission Structure

Permissions follow the format: `{domain}.{action}[.{subaction}]`, with an optional resource identifier. The permission check system enforces least-privilege access control across all Lua modules.

### Permission Checking Mechanism

Permissions are checked using the `security.Can(context, permission, resource, metadata)` function:

- `context`: The current execution context containing actor and scope information
- `permission`: The permission string (e.g., "db.get")
- `resource`: Optional resource identifier (string) the permission applies to
- `metadata`: Optional additional contextual information

## Security Context

The security context consists of:

1. **Actor**: Entity performing actions (has ID and metadata)
2. **Scope**: Collection of policies defining permissions
3. **Policies**: Rules for authorization decisions

Tokens can be created that include specific actor and scope combinations, enabling fine-grained access control for different clients or services.

## Resource Identifiers

Resource identifiers follow the format `namespace:name` and are used to scope permissions to specific resources. For example:

- Database: `system.db:users_db`
- Filesystem: `system.fs:public`
- Process: `app.process:worker_pool`

## Core Security Modules

### Security Module
- `security.policy.get` - Access policies by ID
- `security.policy_group.get` - Access policy groups
- `security.scope.create` - Create custom scopes
- `security.actor.create` - Create actors with specific IDs
- `security.token_store.get` - Access token store resources

### Function Module
- `funcs.context` - Execute functions with custom context values
- `funcs.security` - Execute functions with custom security context
- `funcs.call` - Call specific functions by ID (target function ID as resource)

## Resource Access Permissions

### File System
- `fs.get` - Access specific filesystem resources (filesystem name as resource)

### Execution
- `exec.get` - Access process executor resources (executor ID as resource)
- `exec.run` - Execute specific commands (command string as resource)

### Database
- `db.get` - Access database resources (database ID as resource)

### Storage
- `store.get` - Access key-value store resources (store ID as resource)
- `store.key.get` - Read specific keys (key as resource)
- `store.key.set` - Write specific keys (key as resource)
- `store.key.delete` - Delete specific keys (key as resource)
- `store.key.has` - Check key existence (key as resource)

### Cloud Storage
- `cloudstorage.get` - Access cloud storage resources (resource ID as resource)

## Communication Permissions

### Events
- `events.subscribe` - Subscribe to events from specific systems (system name as resource)

### HTTP Client
- `http_client.request` - Make HTTP requests to specific URLs (URL as resource)

### WebSocket
- `websocket.connect` - Establish WebSocket connections
- `websocket.connect.url` - Connect to specific WebSocket URLs (URL as resource)

### Environment Variables
- `env.get` - Access specific environment variables (variable name as resource)

## Process Permissions

### Process Lifecycle
- `process.spawn` - Spawn new processes (process ID as resource)
- `process.spawn.monitored` - Spawn monitored processes (process ID as resource)
- `process.spawn.linked` - Spawn linked processes (process ID as resource)
- `process.terminate` - Terminate specific processes (PID as resource)
- `process.cancel` - Cancel specific processes (PID as resource)

### Process Context
- `process.context` - Spawn processes with custom context
- `process.security` - Spawn processes with custom security context

### Process Topology
- `process.send` - Send messages to specific processes (PID as resource)
- `process.monitor` - Monitor specific processes (PID as resource)
- `process.unmonitor` - Stop monitoring specific processes (PID as resource)
- `process.link` - Link to specific processes (PID as resource)
- `process.unlink` - Unlink from specific processes (PID as resource)

### Process Registry
- `process.registry.register` - Register process names (name as resource)
- `process.registry.unregister` - Unregister process names (name as resource)

## System Permissions

### Registry
- `registry.apply` - Apply registry changes
- `registry.get` - Access specific registry entries (entry ID as resource)
- `registry.apply_version` - Apply specific registry versions

### System Information
- `system.read` - Read system information (resource types: "memory", "goroutines", "hostname", "pid", "cpu", "gomaxprocs", "gc_percent")
- `system.gc` - Force garbage collection (resource: "gc" or "gc_percent")
- `system.control` - Control system settings (resource: "gomaxprocs")

## Permission Handling Examples

### Rejection Example

When permissions are not granted, operations fail explicitly:

```lua
-- If security.Can(context, "db.get", "system.db:users", nil) returns false
local db = sql.get("system.db:users")
-- Results in: Error: not allowed to access database: system.db:users
```

### Security Context Separation

Security contexts are strictly separated from application contexts:

```lua
-- Create function executor with application context (requires permission)
local executor = funcs.new():with_context({ app_data = "value" }) 

-- Set security context (requires elevated permission)
executor = executor:with_actor(security.new_actor("admin"))
executor = executor:with_scope(admin_scope)
```

Security contexts cannot be removed, and modification attempts without proper permissions result in errors.

## Implementation Details

1. Security checks are performed before any sensitive operation
2. Permission checks are non-bypassable
3. Default deny policy - permissions must be explicitly granted
4. Resource IDs are validated before permission checks
5. PID resolution includes security checks for both direct PID access and registry name lookups
6. Contextual operations preserve security context boundaries

## Best Practices

1. Use the most specific permission possible
2. Include resource identifiers to scope permissions to specific resources
3. Separate application context from security context permissions
4. Validate inputs before security checks to prevent confused deputy problems
5. Never store sensitive information in application context values