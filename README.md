# Pony Runtime

The Pony Runtime is a powerful Golang-based system where AI agents autonomously build and manage software. These agents,
operating within secure, sandboxed environments, dynamically generate code, orchestrate workflows, and handle state
management with versioning and rollback capabilities. Pony integrates with external services and features a plugin
system for extensibility. It empowers agents with knowledge management tools like graph databases and vector embeddings
for intelligent code understanding. Designed for developers, Pony offers an SDK, intuitive configuration, and automated
testing, streamlining the creation of adaptive, robust, and intelligent applications. With Pony, you can automate tasks,
build self-evolving systems, and explore the future of AI-driven software development.

TODO Items:

- security layer 1w - baseline - 8h
- testing runner 1w
- process system 2w (!!!!)
- process interface and layers 2d
- redo terminal registration 6h
- add factory flavors 6h
- redo temporal integration POC to working model (after process model) 1w
- terminating flow 5h - deal with http actually
- update flow 2d (flush and get funcs on demand)
- ns support 1d - executor and processes + ctx
- import app support 3d - need registry api contract
- export state support 2d
- metrics 4d (cut later or expose module)
- reg api 1d (internal after update and hot reload and export)
- ERROR PROPAGATION FROM LUA 2d
- stream in lua plus coro test
- connector to process system from functions

WORK WITH CODEBASE:

- better lua dep manager 2d
- do we preload modules or not? / we need clear require!
- import func 2h
- library folder aliases 4h
- 2CP and batch re-compile

MODULES:

- DONE - redo graph ? 1d
- sql 4d
- migrations
- graph 4d
- memory system 1w
- terminal restart fails 4h
- TESTS MORE TESTS! 10d
- space char (only " " works) 1h
  DONE - uuid lib also 1h
  DONE - finish list delegate 2h

- stabilize system
- update registry to support namespaces
- update registry to add 2CP commits for compile phases

- normalize function definitions (? or hook into vm)

- otel later
- python sdk
  done - fix list helper methods
- wippy cloud and integration
- clean exit is not working! http server stucks! need to collapse ctx when exiting

--------------------- AI VERSION ---------------------
Got it. Here's the organized breakdown:

CORE INFRASTRUCTURE:

- security layer 1w - baseline - 8h
- ns support 1d - executor and processes + ctx
- metrics 4d (cut later or expose module)
- 2CP and batch re-compile
- update registry to support namespaces
- update registry to add 2CP commits for compile phases
- reg api 1d (internal after update and hot reload and export)

PROCESS & RUNTIME:

- process system 2w (!!!!)
- process interface and layers 2d
- terminating flow 5h - deal with http actually
- update flow 2d (flush and get funcs on demand)
- clean exit is not working! http server stucks! need to collapse ctx when exiting
- redo temporal integration POC to working model (after process model) 1w
- connector to process system from functions

LUA SYSTEM & INTEGRATION:

- ERROR PROPAGATION FROM LUA 2d
- stream in lua plus coro test
- better lua dep manager 2d
- do we preload modules or not? / we need clear require!
- normalize function definitions (? or hook into vm)
- import func 2h
- library folder aliases 4h
- ns support

STATE & DATA MANAGEMENT:

- DONE - redo graph ? 1d
- sql 4d
- migrations
- graph 4d
- memory system 1w
- import app support 3d - need registry api contract
- export state support 2d

TERMINAL & UI:

- redo terminal registration 6h
- add factory flavors 6h
- terminal restart fails 4h
- space char (only " " works) 1h
- DONE - uuid lib also 1h
- DONE - finish list delegate 2h

TESTING & STABILITY:

- testing runner 1w
- TESTS MORE TESTS! 10d
- stabilize system

EXTERNAL & SDK:

- otel later
- python sdk
- wippy cloud and integration

Want me to add estimated timelines for each category or reorganize any parts?


```yaml
kind: namespace
name: prod.eu
meta:
  comment: "EU Production Environment"

modules: [ ]
libraries: [ ]

# ns.yaml could also include hot-reload settings
kind: namespace
name: prod.eu
options:
  hot_reload: true
  strict_resolution: true # for cross-ns calls
  propagate_context: true # for process inheritance
```

