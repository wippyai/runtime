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

## Architecture Overview

Wippy's architecture is built on several key pillars:

*   **Process System:** Manages isolated Lua processes with supervision, message passing, coroutine-based concurrency, and location-transparent addressing.
*   **Registry System:** A versioned store for all component definitions, configurations, and metadata, enabling transactional updates and history tracking.
*   **Security Layer:** Enforces fine-grained, policy-based access control across all operations and resources. *Part of the core governance mechanism.*
*   **Communication Infrastructure:** Supports direct messaging, pub/sub patterns (via channels and events), and integrates natively with HTTP/WebSockets.
*   **Resource Management:** Provides centralized, controlled access to databases, file systems, external APIs, and caches. *Part of the core governance mechanism.*

## Key Features

*   **Dynamic Code Loading:** Hot-swap component implementations without service disruption.
*   **Lua Runtime Environment:** Leverage a flexible scripting environment with coroutines for concurrency.
*   **Channel-Based Concurrency:** Utilize Go-like channels for managing concurrent operations and communication between coroutines safely and efficiently.
*   **HTTP Services:** Define dynamic API endpoints, webhooks, and WebSocket connections.
*   **AI Integration:** Standardized LLM interfaces, tool management, prompt engineering support, and vector operations.
*   **Supervision System:** Ensures resilience through automatic process restarts and configurable strategies. *Part of the core governance mechanism.*
*   **Self-Introspection & Modification:** Enables components to query the runtime state and update themselves or other parts of the system dynamically, within governed limits.

## Why Wippy?

Wippy addresses common challenges in modern software development, especially when integrating AI:

*   **Agility:** Rapidly iterate on AI agents, integrations, or features without complex deployment cycles.
*   **Isolation & Control:** Safely test experimental features or customer-specific logic without impacting core stability, thanks to strong isolation and governance.
*   **Maintainability:** Decouple extensions and AI logic from monolithic applications, simplifying updates and reducing technical debt.
*   **Adaptability:** Build systems capable of self-optimization and evolution driven by AI agents or operational data, operating within secure boundaries.
*   **Centralized Management:** Unify the control, monitoring, and governance of diverse integrations and extensions.
*   **Efficient Concurrency:** Manage complex, asynchronous operations reliably using a proven channel-based model.

## Common Use Cases

*   **AI Agent Platform:** Deploy, manage, and rapidly update multiple specialized AI agents under controlled policies.
*   **Customer-Specific Customizations:** Implement bespoke logic or integrations for individual tenants in SaaS applications with enforced resource limits.
*   **Integration Hub:** Create resilient adapters and transformation pipelines between disparate systems with centralized monitoring and security.
*   **Feature Experimentation:** Safely roll out and test new features to targeted user segments in production, with clear boundaries.
*   **Multi-Tenant Processing:** Run isolated, customizable data processing logic for different tenants with resource governance.

## Getting Started

### Prerequisites

- Go 1.21 or later
- Git

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

### Dependency Management

Wippy uses a lock file system similar to `go mod` or `npm` for managing dependencies. The system supports installing, updating, and managing module dependencies with version control.

#### Basic Commands

**Install dependencies from lock file:**
```bash
# Install dependencies from wippy.lock file
./wippy --install app/

# Install with custom lock file
./wippy --install --lock-file=custom.lock app/

# Install with verbose logging
./wippy -v --install app/
```

**Update dependencies to latest versions:**
```bash
# Update all dependencies and regenerate lock file
./wippy --update app/

# Update with custom lock file
./wippy --update --lock-file=custom.lock app/

# Update with verbose logging
./wippy -v --update app/
```

**Run application with dependency management:**
```bash
# Run application (will install dependencies if needed)
./wippy app/

# Run with custom lock file
./wippy --lock-file=custom.lock app/

# Run with verbose logging
./wippy -v app/
```

#### Lock File Format

The `wippy.lock` file contains dependency information in YAML format:

```yaml
directory: .wippy
modules:
- name: wippy/llm
  version: v0.0.11
- name: wippy/security
  version: v0.0.7
- name: wippy/terminal
  version: v0.0.7
- name: wippy/test
  version: v0.0.8
```

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

#### Example Application Configuration

Create `app/app.yaml`:

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

#### Usage Scenarios

**Scenario 1: New Project Setup**
```bash
# 1. Create application directory
mkdir my-app && cd my-app

# 2. Create app.yaml with dependencies
# (see example above)

# 3. Install dependencies
./wippy --install .

# 4. Run application
./wippy .
```

**Scenario 2: Updating Dependencies**
```bash
# 1. Update to latest versions
./wippy --update app/

# 2. Review changes in wippy.lock
cat app/wippy.lock

# 3. Test with updated dependencies
./wippy app/
```

**Scenario 3: Using Custom Lock File**
```bash
# 1. Create production lock file
./wippy --update --lock-file=production.lock app/

# 2. Deploy with production lock file
./wippy --lock-file=production.lock app/
```

**Scenario 4: Development with Multiple Lock Files**
```bash
# Development environment
./wippy --update --lock-file=dev.lock app/

# Staging environment  
./wippy --update --lock-file=staging.lock app/

# Production environment
./wippy --update --lock-file=prod.lock app/
```

**Scenario 5: CI/CD Pipeline**
```bash
# Install dependencies in CI
./wippy --install app/

# Run tests
./wippy app/ --test

# Deploy with specific lock file
./wippy --lock-file=ci.lock app/
```

#### Command Line Options

| Option | Description | Example |
|--------|-------------|---------|
| `--install` | Install dependencies from lock file | `./wippy --install app/` |
| `--update` | Update dependencies and regenerate lock file | `./wippy --update app/` |
| `--lock-file` | Specify custom lock file path | `./wippy --lock-file=custom.lock app/` |
| `-v, --verbose` | Enable verbose logging | `./wippy -v --install app/` |
| `--help` | Show help information | `./wippy --help` |

#### Dependency Resolution

- **Install**: Reads `wippy.lock` and installs exact versions
- **Update**: Resolves latest versions and updates lock file
- **Run**: Automatically installs dependencies if `.wippy` folder is missing

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

#### Troubleshooting

**Dependencies not installing:**
```bash
# Check if .wippy directory exists
ls -la .wippy/

# Reinstall dependencies
rm -rf .wippy
./wippy --install app/
```

**Lock file issues:**
```bash
# Remove lock file and regenerate
rm app/wippy.lock
./wippy --update app/
```

**Module not found:**
```bash
# Check module registry
./wippy --update app/ 2>&1 | grep "module not found"

# Verify module name and version in app.yaml
cat app/app.yaml
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
./wippy --install .
./wippy .
```

5. **Test the application:**
```bash
curl http://localhost:8080
# Output: Hello, Wippy!
```

### Next Steps

- Explore the [Wippy documentation](https://docs.wippy.ai)
- Check out [example applications](https://github.com/wippyai/examples)
- Join the [community discussions](https://github.com/wippyai/runtime/discussions)

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

3. **Run tests:**
```bash
go test ./...
```

### Working with Dependencies

When developing Wippy Runtime itself, you may need to work with the dependency management system:

**Testing dependency commands:**
```bash
# Test install command
./wippy --install app/

# Test update command  
./wippy --update app/

# Test with custom lock file
./wippy --lock-file=test.lock app/
```

**Debugging dependency issues:**
```bash
# Enable verbose logging
./wippy -v --install app/ 2> debug.log

# Check lock file format
cat app/wippy.lock

# Verify module installation
ls -la .wippy/
```

### Code Style

- Follow Go conventions and use `gofmt`
- Add tests for new functionality
- Update documentation for new features
- Use meaningful commit messages

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

### Submitting Changes

1. Create a feature branch: `git checkout -b feature/your-feature`
2. Make your changes and add tests
3. Commit with clear messages: `git commit -m "Add dependency management system"`
4. Push and create a pull request
5. Ensure all tests pass and documentation is updated

### Areas for Contribution

- **Dependency Management**: Enhance the lock file system, add new commands
- **Module Registry**: Improve module discovery and version resolution
- **Documentation**: Add examples, improve guides, fix typos
- **Testing**: Add more test cases, improve coverage
- **Performance**: Optimize module loading and installation
- **Security**: Enhance dependency verification and validation

### Questions or Issues?

- [Open an issue](https://github.com/wippyai/runtime/issues) for bugs or feature requests
- [Join discussions](https://github.com/wippyai/runtime/discussions) for questions
- Check existing issues before creating new ones

## License

This project is licensed under the Mozilla Public License 2.0 - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Thanks to all contributors who have helped build Wippy Runtime
- Inspired by modern dependency management systems like `go mod` and `npm`
- Built with Go and Lua for performance and flexibility

[documentation]: https://docs.wippy.ai
