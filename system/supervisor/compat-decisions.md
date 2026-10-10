# Supervisor retry compatibility decisions

## Exponential backoff

The controller retains its backoff calculator across consecutive unstable
failures. With jitter disabled, delays are initial delay, initial delay times
factor, and so on, capped by max delay. Previously it rebuilt the calculator for
every retry and always waited the initial interval.

The sequence resets after the existing stable threshold, on an explicit new
start, and for an intentional outdated-code restart. Planned restarts do not
consume failure backoff. Existing max-attempt counting and terminal-error rules
remain unchanged. Retry timers remain cancelable, and canceled queued retries
cannot start a process after Stop.

Backoff multiplication and jitter saturate before converting to a duration, so
large factors or many retries cannot overflow into negative/immediate delays.
The configured max delay continues to cap the base interval; jitter keeps its
existing symmetric policy rather than changing that limit's meaning.

## Optional restart intensity

This is an explicit new policy, not an implied change to max_attempts. Omitted or
null intensity disables the window limit; existing configurations gain no new
restart cap. Applications opt in with:

```yaml
lifecycle:
  restart:
    intensity:
      max_restarts: 3
      window: 1m
```

Only admitted failure-driven retries count. Initial/manual starts, rejected or
canceled retries, and planned outdated-code restarts do not. At most max_restarts
such retries may start in the half-open rolling interval (now-window, now].
Running past stable_threshold resets consecutive-failure backoff/counting but
does not erase the window history. History is local to a controller generation;
a registry replacement creates a new generation and a new budget.

Exceeding the limit leaves the service exited with a typed, non-retryable
Unavailable error and its desired state still running. It does not schedule an
automatic wakeup after the window; an explicit later start can resume work.
Both values must be positive when intensity is present. Storage grows lazily to
at most max_restarts timestamps; disabled intensity stores no timestamps.
