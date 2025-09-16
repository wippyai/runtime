<p align="center">
    <a href="https://wippy.ai" target="_blank">
        <picture>
            <source media="(prefers-color-scheme: dark)" srcset="https://github.com/wippyai/.github/blob/main/logo/wippy-text-dark.svg?raw=true">
            <img width="30%" align="center" src="https://github.com/wippyai/.github/blob/main/logo/wippy-text-light.svg?raw=true" alt="Wippy logo">
        </picture>
    </a>
</p>
<h1 align="center">Runtime</h1>
<div align="center">

[![Documentation](https://img.shields.io/badge/documentation-0F6640.svg?style=for-the-badge&logo=gitbook)][documentation]
[![CLI Commands](https://img.shields.io/badge/CLI-Commands-blue.svg?style=for-the-badge&logo=terminal)](#new-cli-command-structure)

</div>

Wippy Runtime provides a dedicated environment for deploying, managing, and dynamically updating software components, particularly AI agents and system extensions, without disrupting your core infrastructure. It's designed for agility, isolation, control, and continuous evolution.

Think of Wippy as a live, adaptable layer integrated with your systems. It allows specialized components and AI agents to operate, learn, and even modify the system itself—all within defined boundaries—in response to new requirements or observed patterns.

## Core Concepts

Wippy enables systems that can understand their own structure and adapt over time, while maintaining stability and security. Key concepts include:

*   **Isolation:** Components run in lightweight processes, preventing failures from cascading and ensuring security boundaries.
*   **Dynamic Updates:** Code and configuration can be updated live via a versioned registry, eliminating downtime for many changes.
*   **Introspection & Adaptation:** The platform and its agents can inspect the system's composition and operational state, enabling automated analysis, optimization recommendations, and self-modification.
*   **AI-Native Integration:** Built-in features facilitate seamless integration with Large Language Models (LLMs), vector databases, and agentic workflows.
*   **Governance:** Robust mechanisms control component behavior, resource access (via the Security Layer and Resource Management), and lifecycle (via the Supervision System), ensuring predictable and secure operation even during dynamic updates.
*   **Concurrency Model:** Utilizes coroutines and Go-inspired channels for efficient, non-blocking concurrency within and between processes.
*   **Command-Based Interface:** Modern CLI architecture with intuitive commands for dependency management and system operations.
*   **Module Replacements:** Local development support through module replacement system for faster iteration cycles.
*   **Default Values for Requirements:** Module requirements can define default values, making the `parameters` section in `ns.dependency` optional and improving developer experience.

## Architecture Overview

Wippy's architecture is built on several key pillars:

*   **Process System:** Manages isolated Lua processes with supervision, message passing, coroutine-based concurrency, and location-transparent addressing.
*   **Registry System:** A versioned store for all component definitions, configurations, and metadata, enabling transactional updates and history tracking.
*   **Security Layer:** Enforces fine-grained, policy-based access control across all operations and resources. *Part of the core governance mechanism.*
*   **Communication Infrastructure:** Supports direct messaging, pub/sub patterns (via channels and events), and integrates natively with HTTP/WebSockets.
*   **Resource Management:** Provides centralized, controlled access to databases, file systems, external APIs, and caches. *Part of the core governance mechanism.*
*   **CLI System:** Modern command-based interface for dependency management, module operations, and application execution with extensible command architecture.
*   **Module Replacement System:** Integrated replacement management for local development and testing workflows.

## Key Features

*   **Dynamic Code Loading:** Hot-swap component implementations without service disruption.
*   **Lua Runtime Environment:** Leverage a flexible scripting environment with coroutines for concurrency.
*   **Channel-Based Concurrency:** Utilize Go-like channels for managing concurrent operations and communication between coroutines safely and efficiently.
*   **HTTP Services:** Define dynamic API endpoints, webhooks, and WebSocket connections.
*   **AI Integration:** Standardized LLM interfaces, tool management, prompt engineering support, and vector operations.
*   **Supervision System:** Ensures resilience through automatic process restarts and configurable strategies. *Part of the core governance mechanism.*
*   **Self-Introspection & Modification:** Enables components to query the runtime state and update themselves or other parts of the system dynamically, within governed limits.
*   **Modern CLI Interface:** Command-based CLI with intuitive commands for dependency management.
*   **Module Replacements:** Support for local module development and testing with the `replace` command.
*   **Enhanced Lock File System:** Improved lock file format with directory specifications and replacement tracking.
*   **Command Help System:** Comprehensive help for each command with examples and usage patterns.
*   **Extensible Command Architecture:** Easy to add new commands following established patterns.

## Why Wippy?

Wippy addresses common challenges in modern software development, especially when integrating AI:

*   **Agility:** Rapidly iterate on AI agents, integrations, or features without complex deployment cycles.
*   **Isolation & Control:** Safely test experimental features or customer-specific logic without impacting core stability, thanks to strong isolation and governance.
*   **Maintainability:** Decouple extensions and AI logic from monolithic applications, simplifying updates and reducing technical debt.
*   **Adaptability:** Build systems capable of self-optimization and evolution driven by AI agents or operational data, operating within secure boundaries.
*   **Centralized Management:** Unify the control, monitoring, and governance of diverse integrations and extensions.
*   **Efficient Concurrency:** Manage complex, asynchronous operations reliably using a proven channel-based model.
*   **Developer Experience:** Modern CLI interface with intuitive commands for faster development and deployment workflows.
*   **Local Development:** Module replacement system enables rapid local development and testing without registry dependencies.

## Common Use Cases

*   **AI Agent Platform:** Deploy, manage, and rapidly update multiple specialized AI agents under controlled policies.
*   **Customer-Specific Customizations:** Implement bespoke logic or integrations for individual tenants in SaaS applications with enforced resource limits.
*   **Integration Hub:** Create resilient adapters and transformation pipelines between disparate systems with centralized monitoring and security.
*   **Feature Experimentation:** Safely roll out and test new features to targeted user segments in production, with clear boundaries.
*   **Multi-Tenant Processing:** Run isolated, customizable data processing logic for different tenants with resource governance.
*   **Development Workflow:** Streamlined dependency management with modern CLI commands for rapid iteration and testing.
*   **Module Development:** Local module development and testing with replacement system for faster development cycles.

## Module Requirements with Default Values

Wippy introduces a powerful feature that simplifies module configuration by allowing requirements to define default values. This enhancement makes the `parameters` section in `ns.dependency` entries **optional** when modules provide sensible defaults.

### Key Benefits

- **Simplified Configuration**: Applications can use modules without specifying parameters when defaults are available
- **Flexible Override**: Applications can still override defaults when custom values are needed
- **Backward Compatibility**: Existing applications with parameters continue to work unchanged
- **Better Developer Experience**: Less boilerplate configuration and more intuitive module usage

### How It Works

**Module Configuration (with defaults):**
```yaml
# Module (wippy/llm/_index.yaml)
version: "1.0"
namespace: wippy.llm

entries: 
  - name: application_host
    kind: ns.requirement
    meta: 
      description: "Host ID for the application processes"
    targets: 
      - entry: token_refresh
        path: .meta.default_host
    default: "app:processes"  # Default value makes parameters optional
```

**Application Configuration (simplified):**
```yaml
# Application (_index.yaml) - Parameters section is now OPTIONAL
version: "1.0"
namespace: app.example

entries:
  - name: __dependency.wippy.llm
    kind: "ns.dependency"
    component: "wippy/llm"
    version: ">=v0.0.7"
    # No parameters section needed when module has defaults!
```

**Application Configuration (with custom values):**
```yaml
# Application (_index.yaml) - Override defaults when needed
entries:
  - name: __dependency.wippy.llm
    kind: "ns.dependency"
    component: "wippy/llm"
    version: ">=v0.0.7"
    parameters: 
      - name: "application_host"
        value: "system:processes"  # Overrides module default
```

### Behavior Examples

1. **Application provides parameter**: Uses application value (overrides default)
2. **Application doesn't provide parameter**: Uses module default value
3. **Application has no parameters field**: Uses module default value
4. **Application has malformed parameters**: Gracefully falls back to default
5. **No parameter and no default**: Requirement is skipped (graceful degradation)

### Validation Rules

- Parameter matching validation with warning logs for mismatches
- Graceful handling of missing, nil, or malformed parameter fields
- Requirements without defaults are skipped when no parameter is provided
- Default values are extracted from requirement entries automatically

This feature significantly improves the developer experience by reducing configuration complexity while maintaining full flexibility for custom scenarios.

## Getting Started

### Prerequisites

- Go 1.21 or later
- Git
- Basic familiarity with command-line interfaces (similar to git, npm, or docker)

### Installation

1. Clone the repository:
```bash
git clone https://github.com/wippyai/runtime.git
cd runtime
```

2. Build the Wippy runtime:
```bash
go build -o wippy ./cmd/runner/
```

3. Make the binary available in your PATH (optional):
```bash
# Add to PATH for current session
export PATH=$PATH:$(pwd)

# Or create a symlink
sudo ln -s $(pwd)/wippy /usr/local/bin/wippy
```

4. Verify the installation:
```bash
# Check if wippy is available
wippy help

# Or if not in PATH
./wippy help
```

### Dependency Management

Wippy uses a lock file system similar to `go mod` or `npm` for managing dependencies. The system supports installing, updating, and managing module dependencies with version control.

### Quick Start

Get started with Wippy in just a few commands:

```bash
# 1. Initialize a new project
wippy init

# 2. Install dependencies (if you have a wippy.lock file)
wippy install

# 3. Run your application
wippy run

# 4. Get help anytime
wippy help
```

**Need to update dependencies?**
```bash
wippy update
```

**Working with local modules?**
```bash
# Add a local replacement
wippy replace add wippy/llm ./local/llm

# List current replacements
wippy replace list
```

**Using custom lock files?**
```bash
# Initialize with custom lock file
wippy init --lock-file=production.lock --src-dir=src --modules-dir=modules --

# Install from custom lock file
wippy install --lock-file=production.lock --

# Run with custom lock file
wippy run --lock-file=production.lock --
```

## New CLI Command Structure

Wippy now uses a modern command-based CLI interface (similar to `git` or `npm`) instead of the legacy flag-based approach. This new architecture provides better user experience, clearer command semantics, and improved extensibility.

### Benefits of the New CLI

- **Intuitive Commands**: Commands like `wippy init`, `wippy install`, and `wippy update` are more intuitive than flag-based options
- **Better Help System**: Each command has its own help documentation (`wippy <command> --help`)
- **Consistent Interface**: All commands follow the same pattern with options and arguments
- **Extensible**: Easy to add new commands without cluttering the main help output
- **Modern UX**: Follows established patterns from popular tools like `git`, `npm`, and `docker`

### Command Structure

All commands follow this pattern:
```bash
wippy <command> [options] [--] [arguments]
```

- `command`: The action to perform (init, install, update, run, replace)
- `options`: Command-specific flags (--lock-file, --src-dir, etc.)
- `--`: Separator between options and arguments (required for some commands)
- `arguments`: Command-specific arguments (e.g., module names for replace)

### Available Commands

**Initialize a new lock file:**
```bash
# Initialize with default settings
wippy init

# Initialize with custom paths
wippy init --lock-file="./wippy.lock" --src-dir="." --modules-dir=".wippy" --

# Initialize with custom lock file name
wippy init --lock-file="custom.lock" --src-dir="src" --modules-dir="modules" --
```

**Install dependencies from lock file:**
```bash
# Install dependencies from wippy.lock file
wippy install

# Install with custom lock file
wippy install --lock-file="custom.lock" --

# Install with verbose logging
wippy install -v --
```

**Update dependencies to latest versions:**
```bash
# Update all dependencies and regenerate lock file
wippy update

# Update with custom lock file
wippy update --lock-file="custom.lock" --

# Update with verbose logging
wippy update -v --
```

**Run application with dependency management:**
```bash
# Run application (will install dependencies if needed)
wippy run

# Run with custom lock file
wippy run --lock-file="custom.lock" --

# Run with verbose logging
wippy run -v --

# Run with performance profiling enabled
wippy run -p --

# Run with very verbose logging and profiling
wippy run -vv -p --

# Run with embedded files
wippy run --use-embed --

# Run with cluster membership enabled
wippy run --cluster --cluster-name="my-node" --

# Run with custom cluster configuration
wippy run --cluster --cluster-bind="0.0.0.0" --cluster-port=7946 --cluster-join="node1:7946,node2:7946" --
```

**Manage module replacements:**
```bash
# List current replacements
wippy replace list

# Add a replacement for a module
wippy replace add wippy/llm ./local/llm

# Remove a replacement
wippy replace remove wippy/llm

# Use custom lock file
wippy replace --lock-file="custom.lock" -- add wippy/security ./local/security
```

**Get help:**
```bash
# Show general help
wippy help

# Show command-specific help
wippy init --help
wippy install --help
wippy update --help
wippy run --help
wippy replace --help
```

### Run Command Advanced Options

The `wippy run` command supports additional flags for advanced configuration:

**Performance and Debugging:**
```bash
# Enable performance profiling (pprof server on localhost:6060)
wippy run -p --

# Enable very verbose logging with stack traces
wippy run -vv --

# Combine profiling and verbose logging
wippy run -vv -p --

# Use embedded files instead of file system
wippy run --use-embed --
```

**Cluster Configuration:**
```bash
# Enable cluster membership with default settings
wippy run --cluster --

# Custom cluster node name
wippy run --cluster --cluster-name="production-node" --

# Custom cluster bind address and port
wippy run --cluster --cluster-bind="192.168.1.100" --cluster-port=8000 --

# Join existing cluster
wippy run --cluster --cluster-join="node1:7946,node2:7946,node3:7946" --

# Cluster with secret authentication
wippy run --cluster --cluster-secret="base64-encoded-secret" --

# Cluster with secret from file
wippy run --cluster --cluster-secret-file="/path/to/secret.txt" --

# Custom cluster advertise address
wippy run --cluster --cluster-advertise="10.0.0.100" --

# Complete cluster configuration
wippy run --cluster \
  --cluster-name="worker-1" \
  --cluster-bind="0.0.0.0" \
  --cluster-port=7946 \
  --cluster-join="master:7946,worker-2:7946" \
  --cluster-secret-file="/etc/wippy/cluster-secret" \
  --cluster-advertise="192.168.1.10" --
```

**Combined Examples:**
```bash
# Development with profiling and verbose logging
wippy run -vv -p --lock-file="dev.lock" --

# Production cluster node with custom configuration
wippy run --cluster --cluster-name="prod-node-1" --cluster-join="prod-master:7946" -p --

# Testing with embedded files and cluster
wippy run --use-embed --cluster --cluster-name="test-node" -vv --
```

### Command Options

| Option | Description | Example |
|--------|-------------|---------|
| `--lock-file` | Specify custom lock file path | `wippy install --lock-file=custom.lock --` |
| `--src-dir` | Source directory path (init only) | `wippy init --src-dir=src --` |
| `--modules-dir` | Modules directory path (init only) | `wippy init --modules-dir=modules --` |
| `-v, --verbose` | Enable verbose logging | `wippy install -v --` |
| `-vv` | Enable very verbose logging with stack traces | `wippy run -vv --` |
| `-p, --profiling` | Enable performance profiling (run only) | `wippy run -p --` |
| `--use-embed` | Use embedded files (run only) | `wippy run --use-embed --` |
| `--cluster` | Enable cluster membership (run only) | `wippy run --cluster --` |
| `--cluster-name` | Cluster node name (run only) | `wippy run --cluster --cluster-name=node1 --` |
| `--cluster-bind` | Cluster bind address (run only) | `wippy run --cluster --cluster-bind=0.0.0.0 --` |
| `--cluster-port` | Cluster bind port (run only) | `wippy run --cluster --cluster-port=7946 --` |
| `--cluster-join` | Comma-separated addresses to join (run only) | `wippy run --cluster --cluster-join=node1:7946,node2:7946 --` |
| `--cluster-secret` | Cluster secret key (run only) | `wippy run --cluster --cluster-secret=base64key --` |
| `--cluster-secret-file` | Path to cluster secret file (run only) | `wippy run --cluster --cluster-secret-file=secret.txt --` |
| `--cluster-advertise` | Cluster advertise IP (run only) | `wippy run --cluster --cluster-advertise=192.168.1.100 --` |
| `--help` | Show help information | `wippy init --help` |

### Legacy Flag Support

**Note**: The legacy flag-based dependency management (`--install`, `--update`) is no longer supported. Please use the new command format shown above.

### Migration Guide

If you're upgrading from the legacy CLI, here's how to migrate your existing workflows:

**Old Command → New Command**
```bash
# Install dependencies
./wippy --install app/          → wippy install
./wippy --install --lock-file=custom.lock app/  → wippy install --lock-file=custom.lock --

# Update dependencies  
./wippy --update app/           → wippy update
./wippy --update --lock-file=custom.lock app/   → wippy update --lock-file=custom.lock --

# Run application
./wippy app/                    → wippy run
./wippy --lock-file=custom.lock app/            → wippy run --lock-file=custom.lock --

# Verbose logging
./wippy -v --install app/       → wippy install -v --
./wippy -v --update app/        → wippy update -v --
```

**Key Changes:**
- Remove the `./wippy` prefix and use `wippy` directly
- Replace `--install` with `install` command
- Replace `--update` with `update` command  
- Add `--` separator before arguments when needed
- Use `wippy run` instead of `./wippy <path>`

#### Lock File Format

The `wippy.lock` file contains dependency information in YAML format:

```yaml
directories:
  modules: .wippy
  src: .
modules:
- name: wippy/llm
  version: v0.0.11
- name: wippy/security
  version: v0.0.7
- name: wippy/terminal
  version: v0.0.7
- name: wippy/test
  version: v0.0.8
replacements:
- from: wippy/llm
  to: ./local/llm
```

**Key changes in the new format:**
- `directories` section specifies source and modules directories
- `replacements` section manages module replacements
- Paths are relative to the lock file location

#### Application Structure

Create your Wippy application with the following structure:

```
app/
├── app.yaml          # Main application configuration
├── wippy.lock        # Dependency lock file (auto-generated)
├── src/              # Application source code
│   ├── chat/
│   ├── http/
│   ├── tools/
│   └── ...
└── public/           # Static assets
    ├── index.html
    └── ...
```

**Initializing the structure:**
```bash
# Create the basic structure
mkdir my-app && cd my-app
mkdir src public

# Initialize with wippy
wippy init --src-dir=src --modules-dir=.wippy --

# This creates wippy.lock with the specified directory structure
```

#### Example Application Configuration

Create `app.yaml`:

```yaml
name: my-wippy-app
version: 1.0.0

# Declare dependencies
dependencies:
  - name: wippy/llm
    version: v0.0.11
  - name: wippy/security
    version: v0.0.7
  - name: wippy/terminal
    version: v0.0.7

# Application services
services:
  - name: http
    type: http
    config:
      port: 8080
      routes:
        - path: /
          handler: src/http/main.lua

  - name: chat
    type: process
    config:
      entry: src/chat/manager.lua
```

**Setting up the application:**
```bash
# 1. Create app.yaml (see above)

# 2. Initialize lock file
wippy init --src-dir=src --modules-dir=.wippy --

# 3. Update dependencies (resolves versions from app.yaml)
wippy update

# 4. Install dependencies
wippy install

# 5. Run the application
wippy run
```

#### Usage Scenarios

**Scenario 1: New Project Setup**
```bash
# 1. Create application directory
mkdir my-app && cd my-app

# 2. Create app.yaml with dependencies
# (see example above)

# 3. Initialize lock file with custom structure
wippy init --src-dir=src --modules-dir=.wippy --

# 4. Update dependencies (resolves from app.yaml)
wippy update

# 5. Install dependencies
wippy install

# 6. Run application
wippy run
```

**Scenario 2: Updating Dependencies**
```bash
# 1. Update to latest versions
wippy update

# 2. Review changes in wippy.lock
cat wippy.lock

# 3. Test with updated dependencies
wippy run

# 4. If issues arise, rollback to previous lock file
# (you can keep multiple lock files for different environments)
```

**Scenario 3: Using Custom Lock File**
```bash
# 1. Create production lock file
wippy update --lock-file=production.lock --

# 2. Deploy with production lock file
wippy run --lock-file=production.lock --

# 3. Verify production deployment
wippy run --lock-file=production.lock -- --status
```

**Scenario 4: Development with Multiple Lock Files**
```bash
# Development environment
wippy update --lock-file=dev.lock --

# Staging environment  
wippy update --lock-file=staging.lock --

# Production environment
wippy update --lock-file=prod.lock --

# Switch between environments
wippy run --lock-file=dev.lock --    # Development
wippy run --lock-file=staging.lock -- # Staging
wippy run --lock-file=prod.lock --    # Production
```

**Scenario 5: CI/CD Pipeline**
```bash
# Install dependencies in CI
wippy install

# Run tests
wippy run

# Deploy with specific lock file
wippy run --lock-file=ci.lock --

# Validate lock file integrity
wippy install --lock-file=ci.lock -- --validate
```

**Scenario 6: Module Replacements**
```bash
# 1. Add a local replacement for development
wippy replace add wippy/llm ./local/llm

# 2. List current replacements
wippy replace list

# 3. Test with local module
wippy run

# 4. Remove replacement when done
wippy replace remove wippy/llm

# 5. Verify removal
wippy replace list
```

**Scenario 7: Development Workflow**
```bash
# 1. Start development session
wippy init --lock-file=dev.lock --src-dir=src --modules-dir=.wippy --

# 2. Add local module for testing
wippy replace add wippy/test ./local/test

# 3. Update and test
wippy update --lock-file=dev.lock --
wippy run --lock-file=dev.lock --

# 4. Commit working changes
git add wippy.lock
git commit -m "Update dependencies and add local test module"

# 5. Clean up for production
wippy replace remove wippy/test
wippy update --lock-file=prod.lock --
```

#### Command Line Options

| Option | Description | Example |
|--------|-------------|---------|
| `--lock-file` | Specify custom lock file path | `wippy install --lock-file=custom.lock --` |
| `--src-dir` | Source directory path (init only) | `wippy init --src-dir=src --` |
| `--modules-dir` | Modules directory path (init only) | `wippy init --modules-dir=modules --` |
| `-v, --verbose` | Enable verbose logging | `wippy install -v --` |
| `--help` | Show help information | `wippy help` |

#### Dependency Resolution

- **`wippy install`**: Reads `wippy.lock` and installs exact versions
- **`wippy update`**: Resolves latest versions and updates lock file
- **`wippy run`**: Automatically installs dependencies if `.wippy` folder is missing
- **`wippy init`**: Creates a new lock file with specified directory structure
- **`wippy replace`**: Manages module replacements for local development

#### Module Installation

Modules are installed in the `.wippy` directory with the following structure:

```
.wippy/
├── wippy/
│   ├── llm@01984154-6ac5-7325-8b52-27edecfe60f4/
│   │   └── module-llm-0.0.11/
│   │       ├── llm.lua
│   │       ├── models.lua
│   │       └── ...
│   ├── security@01978c92-7d02-7b4a-95df-55b57cfe80b7/
│   └── ...
└── other-org/
    └── module@commit-hash/
```

#### Module Replacements

Wippy supports module replacements, similar to Go's `replace` directive. This allows you to use local versions of modules for development or testing purposes.

**Adding a replacement:**
```bash
# Replace a module with a local path
wippy replace add wippy/llm ./local/llm

# Replace with custom lock file
wippy replace --lock-file=custom.lock -- add wippy/security ./local/security
```

**Listing replacements:**
```bash
# Show all current replacements
wippy replace list
```

**Removing replacements:**
```bash
# Remove a specific replacement
wippy replace remove wippy/llm

# Remove with custom lock file
wippy replace --lock-file=custom.lock -- remove wippy/security
```

**Replacement validation:**
- Replacements are validated before installation
- Invalid paths will cause installation to fail
- Replacements are stored in the lock file for consistency

#### Troubleshooting

**Dependencies not installing:**
```bash
# Check if .wippy directory exists
ls -la .wippy/

# Check lock file status
wippy install --help

# Reinstall dependencies
rm -rf .wippy
wippy install

# If issues persist, try reinitializing
rm wippy.lock
wippy init
wippy update
wippy install
```

**Lock file issues:**
```bash
# Remove lock file and regenerate
rm wippy.lock
wippy update

# Check lock file format
wippy init --help

# Validate lock file structure
cat wippy.lock
```

**Module not found:**
```bash
# Check module registry
wippy update 2>&1 | grep "module not found"

# Verify module name and version in app.yaml
cat app.yaml

# Check for module replacements
wippy replace list

# Try updating dependencies
wippy update
```

**CLI command issues:**
```bash
# Get help for specific command
wippy <command> --help

# Check command syntax
wippy help

# Verify command exists
wippy <command> --help 2>&1 | head -1
```

### Running Your First Application

1. **Create a simple application:**
```bash
mkdir hello-wippy
cd hello-wippy
```

2. **Create `app.yaml`:**
```yaml
name: hello-wippy
version: 1.0.0

dependencies:
  - name: wippy/http
    version: v0.0.7

services:
  - name: hello
    type: http
    config:
      port: 8080
      routes:
        - path: /
          handler: src/hello.lua
```

3. **Create `src/hello.lua`:**
```lua
local http = require("http")

return function(req, res)
    res:send("Hello, Wippy!")
end
```

4. **Install and run:**
```bash
wippy install
wippy run
```

5. **Test the application:**
```bash
curl http://localhost:8080
# Output: Hello, Wippy!
```

6. **Stop the application:**
```bash
# Press Ctrl+C in the terminal running wippy run
# Or send SIGTERM to the process
```

### Next Steps

- Explore the [Wippy documentation](https://docs.wippy.ai)
- Check out [example applications](https://github.com/wippyai/examples)
- Join the [community discussions](https://github.com/wippyai/runtime/discussions)
- Try the new CLI commands: `wippy init`, `wippy install`, `wippy update`, `wippy run`, `wippy replace`
- Experiment with module replacements for local development

## Contributing

We welcome contributions to Wippy Runtime! Here's how you can get started:

### Development Setup

1. **Fork and clone the repository:**
```bash
git clone https://github.com/your-username/runtime.git
cd runtime
```

2. **Build the development version:**
```bash
go build -o wippy ./cmd/runner/
```

3. **Test the new CLI interface:**
```bash
# Test help system
./wippy help

# Test init command
./wippy init --lock-file=test.lock --src-dir=src --modules-dir=modules --

# Test other commands
./wippy install --lock-file=test.lock --
./wippy update --lock-file=test.lock --
./wippy run --lock-file=test.lock --
```

4. **Run tests:**
```bash
go test ./...
```

### Working with Dependencies

When developing Wippy Runtime itself, you may need to work with the dependency management system:

**Testing dependency commands:**
```bash
# Test install command
wippy install

# Test update command  
wippy update

# Test with custom lock file
wippy install --lock-file=test.lock --

# Test init command
wippy init --lock-file=test.lock --src-dir=src --modules-dir=modules --

# Test replace command
wippy replace add wippy/test ./local/test
wippy replace list
wippy replace remove wippy/test

# Test run command
wippy run --lock-file=test.lock --

# Test help system
wippy help
wippy init --help
wippy install --help
```

**Testing CLI architecture:**
```bash
# Test command registration
wippy help | grep -E "(init|install|update|run|replace)"

# Test command help consistency
for cmd in init install update run replace; do
    wippy $cmd --help | head -1
done

# Test command execution flow
wippy init --lock-file=test.lock --src-dir=src --modules-dir=modules --
wippy install --lock-file=test.lock --
wippy run --lock-file=test.lock --
```

**Debugging dependency issues:**
```bash
# Enable verbose logging
wippy install -v -- 2> debug.log

# Check lock file format
cat wippy.lock

# Verify module installation
ls -la .wippy/
```

### Code Style

- Follow Go conventions and use `gofmt`
- Add tests for new functionality
- Update documentation for new features
- Use meaningful commit messages
- For CLI commands, follow the established pattern in `cmd/runner/cli.go`
- Ensure each command has proper help text and error handling

### Testing

Run the test suite:
```bash
# Run all tests
go test ./...

# Run specific test
go test ./moduleloader

# Run with coverage
go test -cover ./...
```

**Test CLI functionality:**
```bash
# Test CLI commands
go test ./cmd/runner -v

# Test specific CLI functionality
go test ./cmd/runner -run TestCLI

# Test dependency management
go test ./cmd/runner -run TestDependency
```

### Submitting Changes

1. Create a feature branch: `git checkout -b feature/your-feature`
2. Make your changes and add tests
3. Commit with clear messages: `git commit -m "Add dependency management system"`
4. Push and create a pull request
5. Ensure all tests pass and documentation is updated

**For CLI changes:**
- Test all affected commands: `wippy <command> --help`
- Verify help text consistency across commands
- Test command execution with various flag combinations
- Update this README with any new commands or options

### Areas for Contribution

- **Dependency Management**: Enhance the lock file system, add new commands
- **Module Registry**: Improve module discovery and version resolution
- **CLI Interface**: Add new commands, improve user experience, enhance help system
- **Module Replacements**: Enhance replacement validation, add more replacement types
- **Documentation**: Add examples, improve guides, fix typos
- **Testing**: Add more test cases, improve coverage
- **Performance**: Optimize module loading and installation
- **Security**: Enhance dependency verification and validation

**CLI-specific contributions:**
- **New Commands**: Add commands for common operations (e.g., `wippy status`, `wippy clean`)
- **Command Options**: Enhance existing commands with useful flags
- **Help System**: Improve help text, add examples, create tutorials
- **Error Handling**: Better error messages and recovery suggestions
- **Command Aliases**: Add shortcuts for common operations

### Questions or Issues?

- [Open an issue](https://github.com/wippyai/runtime/issues) for bugs or feature requests
- [Join discussions](https://github.com/wippyai/runtime/discussions) for questions
- Check existing issues before creating new ones

**CLI-specific help:**
- Use `wippy help` for general help
- Use `wippy <command> --help` for command-specific help
- Check the [CLI documentation](https://docs.wippy.ai/cli) for detailed guides
- Report CLI issues with command output and error messages

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Thanks to all contributors who have helped build Wippy Runtime
- Inspired by modern dependency management systems like `go mod` and `npm`
- Built with Go and Lua for performance and flexibility
- CLI design inspired by modern tools like `git`, `npm`, and `docker`
- Module replacement system inspired by Go's `replace` directive

[documentation]: https://docs.wippy.ai
