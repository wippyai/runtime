# Supervisor state-event contract

`service.update` notifications retain their existing kind and `State` payload.
They now contain the controller's actual desired status, retry count, startup
time and last-update time, alongside status and details. A failing service whose
desired state is running stays `Desired=running`; it does not claim that failure
was requested. This corrects observability, not lifecycle decisions.

The public `NewController` status/details callback remains supported. The
supervisor's internal callback receives the whole state snapshot, without looking
the controller up in the supervisor map or acquiring its registry lock.

Process-service definition updates already use transactional `service.register`
replacement on current main. This remains the canonical update path. The manager
creates a new immutable service definition, the supervisor stops the old process,
and an active service resumes on the replacement even when its new definition has
`auto_start=false`. Inactive services remain inactive. No competing update command
or mutable running-service configuration is introduced.

Tests cover exact state payloads, monitor details, and a real manager-to-supervisor
replacement changing process, host and input while preserving active intent.
