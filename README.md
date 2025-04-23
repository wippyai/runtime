# Wippy Runtime

Wippy Runtime is a service platform for running dynamic extensions and AI agents alongside existing infrastructure. It provides an isolated, updatable environment for components without disrupting core applications.

## Architecture

### Process System
- Lightweight process isolation for stability and security
- Message-based communication between processes
- Supervision for automatic recovery from failures
- Location-transparent addressing via process IDs (PIDs)

### Registry System
- Versioned configuration store for component definitions and configurations
- Dynamic updates without service restarts
- Transaction-based changes with history
- Logical namespace organization

### Security Layer
- Policy-based access control
- Fine-grained permissions on resources and actions
- Identity propagation through request chains

### Communication Infrastructure
- Direct process-to-process messaging
- Topic-based publish/subscribe patterns
- HTTP and WebSocket protocol integration
- External system interaction

### Resource Management
- Centralized management of external dependencies:
   - Database connections (PostgreSQL, MySQL, SQLite)
   - File system access
   - External APIs and services
   - Memory stores and caches

## Key Features

- **Dynamic Code Loading**: Load and update components at runtime; hot-swap implementations without disruption; maintain state during updates; version control for deployed components.
- **Lua Runtime Environment**: Script-based components; coroutine-based concurrency; built-in state management.
- **HTTP Services**: Dynamic API endpoint and webhook creation; request routing; middleware chains; WebSocket support.
- **AI Integration**: Standardized LLM interfaces; tool and prompt management; vector operations for semantic processing.
- **Supervision System**: Automatic restart of failed processes; configurable restart strategies; error isolation; health monitoring.
- **Self-Introspection**: Processes and components can inspect their own configuration, state, and runtime environment. Supports runtime queries for metadata, dependencies, and health.
- **Self-Modification**: Components can update their own code and configuration at runtime via the registry, supporting live patching, adaptation, and automated upgrades without requiring manual intervention or restarts.

## Use Cases

1. **AI Agent Platform**
   - Deploy specialized agents as isolated processes
   - Update agent logic and prompts independently
   - Use registry for prompt/config management
   - A/B test agent implementations

2. **Customer-Specific Customizations**
   - Isolated extensions per customer
   - Custom API adapters and business logic
   - Secure management of customer credentials

3. **Integration Hub**
   - Adapter processes for internal systems
   - Data transformation pipelines
   - Protocol translation and centralized configuration

4. **Feature Experimentation Platform**
   - Experimental features as isolated deployments
   - Route specific users to new implementations
   - Collect metrics and enable/disable features at runtime

5. **Multi-Tenant Processing Pipelines**
   - Isolated processing per tenant
   - Tenant-specific rules and security policies
   - Independent updates and scalable resource allocation