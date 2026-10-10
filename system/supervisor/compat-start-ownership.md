# Supervisor start and stop ownership

The controller retains ownership of each asynchronous Start until it returns.
If Start succeeds after timeout or cancellation, the controller stops that
incarnation with an independent stop deadline before allowing another Start.
Retry delays still elapse normally, but retry admission also waits for this
cleanup. Stop itself remains deadline-bounded while a Start is pending.

A failed late cleanup blocks retry and explicit starts with a typed,
non-retryable Unavailable error. It leaves status failed, not exited: failure to
stop is not proof that the child is gone. A later successful explicit Stop clears
the barrier. A failed explicit Stop likewise reports failed instead of stopped.
Canceling a queued caller does not replay its start after cleanup. A failed Stop
also blocks a manual Start until Stop succeeds; reporting a failure alone is not
permission to create another live child.

Process services now broadcast completion on a separate internal channel. Stop
does not consume the controller's terminal status or planned-restart notices.
Run-context cancellation requests child cancellation but is not itself an exit
acknowledgment; the relay monitor remains until the child's terminal event or
relay completion, then removes its monitoring PID and detaches.

The controller's service frame remains retained until startup/late cleanup
workers release ownership, including after controller closure. A service that
ignores both cancellation and deadlines forever cannot be forcibly stopped by
the generic Service interface. In that case ownership is retained and another
incarnation is not started; caller deadlines are still honored.

No new entry option is required. Normal successful start/stop and immutable
registry replacement use the same interfaces. Additional synchronization is
per lifecycle transition, not per actor step or message.
