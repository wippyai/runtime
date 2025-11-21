# Temporal V2 Migration

**Date:** 2025-11-20
**Source Branch:** `feature/temporal`
**Target Branch:** `feature/temporal-v2` (from `feature/scorched-earth`)

## Summary

Cherry-picked all Temporal.io integration files from the `feature/temporal` branch into a new branch based on the latest `feature/scorched-earth` work.

## Files Copied

### API Layer - Temporal Service Definitions

- `api/service/temporal/client.go` - Client configuration (connection, auth, health check)
- `api/service/temporal/events.go` - Event system for Temporal components
- `api/service/temporal/worker.go` - Worker configuration

### Service Layer - Temporal Implementation

#### Client Management
- `service/temporal/client/client.go` - Temporal client wrapper
- `service/temporal/client/factory.go` - Client factory pattern
- `service/temporal/client/logger.go` - Custom logger for Temporal SDK
- `service/temporal/client/manager.go` - Client lifecycle manager
- `service/temporal/system.go` - Main Temporal system component

#### Worker & Task Queue Management
- `service/temporal/worker_manager.go` - Worker lifecycle management
- `service/temporal/task_queue/factory.go` - Task queue factory
- `service/temporal/task_queue/host.go` - Task queue host
- `service/temporal/task_queue/worker.go` - Worker implementation

#### Activity Support
- `service/temporal/activity/listener.go` - Activity registration & execution

#### Workflow Support
- `service/temporal/workflow/command.go` - Workflow command handling
- `service/temporal/workflow/definition.go` - Workflow definitions
- `service/temporal/workflow/factory.go` - Workflow factory
- `service/temporal/workflow/listener.go` - Workflow registration & execution

#### Data Converter (Already existed)
- `service/temporal/dataconverter/converter.go` - Lua ↔ Temporal payload conversion
- `service/temporal/dataconverter/converter_test.go` - Converter tests

### Runtime - Lua Workflow Support

- `runtime/lua/component/workflow/manager.go` - Lua workflow manager
- `runtime/lua/component/workflow/module.go` - Lua `workflow` module
- `runtime/lua/component/workflow/queue.go` - Workflow queue integration
- `runtime/lua/component/workflow/workflow.go` - Workflow execution context

### Application Examples

#### Activity Examples
- `app/src/activity/_index.yaml` - Activity registry definitions
- `app/src/activity/process_data.lua` - Example activity implementation

#### Workflow Examples
- `app/src/workflow/_index.yaml` - Workflow registry definitions
- `app/src/workflow/simple.lua` - Example workflow implementation

#### Library Support
- `app/src/lib/temporal.lua` - Temporal helper library for Lua

## File Not Found

- `runtime/lua/component/workflow/readme` - Referenced in diff but not found in git

## Architecture Overview

### Components

1. **Client Manager** - Manages connections to Temporal service
2. **Worker Manager** - Manages worker pools per task queue
3. **Activity Listener** - Registers Lua functions as Temporal activities
4. **Workflow Listener** - Registers Lua workflows
5. **Data Converter** - Handles serialization between Lua and Temporal

### Integration Flow

```
Registry Entry (workflow/activity)
    ↓
Listener (workflow/activity listener)
    ↓
Registration with Temporal Worker
    ↓
Execution in Lua Runtime
    ↓
Data Converter (Lua ↔ Temporal)
```

## Next Steps

1. Review all copied files for compatibility with current `scorched-earth` architecture
2. Check for dependency conflicts (imports, API changes)
3. Update boot components to include Temporal system
4. Test basic workflow/activity execution
5. Update documentation for Temporal usage in wippy

## Notes

- This migration preserves the complete Temporal integration from 6 months ago
- May require updates to match current API patterns in `scorched-earth`
- Data converter was the only surviving component in `scorched-earth` branch
- Full integration includes client, workers, activities, workflows, and Lua bindings
