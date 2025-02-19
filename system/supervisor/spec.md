# Supervisor System Overview

The Supervisor system is designed to manage the lifecycle of multiple services with complex interdependencies. It
orchestrates service registration, startup, shutdown, and error handling through a set of tightly integrated components.
This document provides an overview of its architecture, core components, and workflows.

## Architecture and Key Responsibilities

At a high level, the Supervisor system:

- **Registers and Manages Services:**  
  Services are registered into the system via a transaction mechanism. Each service is associated with lifecycle
  configurations (timeouts, retry policies, dependency lists, etc.).

- **Coordinates Lifecycle Operations:**  
  The system controls service startup and shutdown while ensuring proper dependency ordering. It supports both automatic
  and manual operations.

- **Monitors and Recovers Services:**  
  If a service fails to start or unexpectedly stops, the system implements retry logic based on a configurable policy
  and gracefully handles terminal failures.

- **Integrates with an Event System:**  
  The Supervisor listens to an event bus, responding to events like registration, start, stop, and removal. This
  event-driven approach allows coordinated operations across distributed services.

## Core Components

### Supervisor

The **Supervisor** is the central coordinator. Its main responsibilities include:

- **Event Handling:**  
  Subscribes to events (from both registry and supervisor systems) to trigger actions such as service registration,
  removal, startup, and shutdown.

- **Transaction Management:**  
  Uses a transactional helper (via a registry transaction helper) to batch service registration and removal changes.
  This ensures consistency when committing configuration changes.

- **Operation Dispatch:**  
  Based on received events, the Supervisor builds lifecycle operations for individual services. It then delegates the
  actual startup and shutdown ordering to the Sequencer.

- **Lifecycle Queries:**  
  Provides methods to query the state of individual services or the entire system, which is critical for monitoring and
  debugging.

### Controller

The **Controller** is responsible for managing the lifecycle of a single service. Key aspects include:

- **Lifecycle Commands:**  
  It encapsulates service start and stop commands as well as handling unexpected failures. These commands are processed
  asynchronously via an internal channel.

- **State Management:**  
  The Controller tracks both the current and desired state of a service. It also manages a retry counter to handle
  transient failures during startup.

- **Retry and Failure Handling:**  
  Upon a failed start, the Controller checks if the error is terminal. For recoverable errors, it schedules retries
  based on a configurable retry policy.

- **Event Notification:**  
  Changes in service state are communicated back via a callback mechanism, allowing higher-level components or external
  systems to be notified.

### Sequencer

The **Sequencer** manages the ordered execution of lifecycle operations based on service dependencies. Its
responsibilities include:

- **Dependency Graph Construction:**  
  It builds a dependency graph from the list of services. For startup operations, it ensures that dependencies are
  started first; for shutdown, it reverses the order.

- **Parallel Execution:**  
  Operations that are independent (i.e., on the same dependency level) are executed in parallel, speeding up the overall
  process while maintaining the correct ordering.

- **Error Propagation:**  
  If any operation in a dependency level fails, the Sequencer propagates the error, allowing the Supervisor to log or
  handle it appropriately.

### Helpers and Internal Utilities

Supporting components include:

- **Internal State Helpers:**  
  These functions manage internal state tracking (such as the current status, details, retry counts, and last update
  timestamps). They ensure thread-safe updates via mutexes.

- **Registry Transaction Helper:**  
  A helper that manages transactions for registering or removing services. It provides methods to begin a transaction,
  register services, commit changes, or discard transactions if an error occurs.

## System Workflow

1. **Event Reception:**  
   The Supervisor subscribes to an event bus and listens for system events (e.g., registration, start, stop). Each event
   is processed and translated into an action.

2. **Transaction and Registration:**  
   When services are registered, a transaction is started. Changes are batched and then committed to ensure consistency
   across the system.

3. **Building Operations:**  
   Based on events and configuration, the Supervisor creates a set of operations (start/stop) for services. It factors
   in dependency information provided in each service’s configuration.

4. **Sequencing Operations:**  
   The Sequencer organizes these operations into dependency levels. It ensures that all prerequisites for a service are
   satisfied before it is started and that services are stopped in the correct order.

5. **Controller Execution:**  
   Each service’s Controller processes its commands asynchronously. It manages state transitions, handles retries for
   recoverable errors, and updates its status.

6. **Monitoring and Logging:**  
   Throughout the process, state changes and errors are logged. The system also exposes APIs to query service states,
   which is useful for debugging and monitoring.

## Example Usage Flow

Consider a scenario where Service A depends on Service B, which in turn depends on Service C:

- **Registration:**  
  The services are registered via the Supervisor, and a transaction is used to commit these changes.

- **Startup:**  
  An event triggers a start operation for Service A. The Supervisor builds operations for A, B, and C. The Sequencer
  calculates the dependency graph, ensuring that C is started first, followed by B, then A.

- **Recovery:**  
  If Service B fails to start on the first attempt, its Controller uses the retry policy to reattempt the start. If the
  maximum retry count is reached, the system marks the service as failed and propagates the failure.

- **Shutdown:**  
  When stopping the services, the Sequencer reverses the dependency order so that A stops first, then B, and finally C.