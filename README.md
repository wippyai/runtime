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
- import app support 3d
- export state support 2d
- metrics 4d (cut later or expose module)
- reg api 1d (internal after update and hot reload and export)
- ERROR PROPAGATION FROM LUA 2d
- stream in lua plus coro test
- connector to process system from functions
- import func 2h
- library folder aliases 4h

- better lua dep manager 2d
- redo graph ? 1d
- sql 4d
- migrations
- graph 4d
- graph refactor or replacement 4h
- memory system 1w
- terminal restart fails 4h
- TESTS MORE TESTS! 10d
- space char (only " " works) 1h
- uuid lib also 1h (DONE)
- finish list delegate 2h
- do we preload modules or not?
- stabilize system


- normalize function definitions (? or hook into vm)
- otel
- python sdk
- fix list helper methods
- wippy cloud and integration
- clean exit is not working! http server stucks! need to collapse ctx when exiting