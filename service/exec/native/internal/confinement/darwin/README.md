# macOS confinement host contract

macOS confined launches use a separately built, signed Seatbelt trampoline.
The runtime starts it suspended, validates its exact dynamic cdhash and hard/
kill code-signing status, and only then resumes it to install the profile and
execute the target. Work directories are walked from entry-owned descriptors
with `openat` and selected with `fchdir`, so renaming or replacing the declared
root cannot redirect a prepared launch.

The supported policy subset is environment ceilings, a private HOME,
single-process wall time, and descendant prevention. With wall/tree control the
Seatbelt profile denies spawning, giving the runtime a singleton process domain;
Stop and timeout report exact exit/signal status and cannot signal a recycled
PID. A substituted signed helper is rejected before any of its instructions run.

Seatbelt path rules are pathname-based and cannot preserve Linux's object-bound
filesystem guarantee across host replacement. Unprivileged macOS also lacks an
aggregate cgroup/Job equivalent for memory and task counts, and the current
profile cannot prove Linux's total socket denial including `socketpair`.
Accordingly filesystem policies, `network: none`, memory/task limits, confined
PTYs, and confined process groups return `CONFINE_UNSUPPORTED`; no requested
guarantee silently degrades.
