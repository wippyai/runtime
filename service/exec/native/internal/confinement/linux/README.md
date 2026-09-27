# Linux confinement host setup

Every confined launch uses user, mount, PID and IPC namespaces, a minimal
verified helper, `no_new_privs`, and seccomp. `network: none` adds a fresh
network namespace and denies socket operations. A restricted `fs` view adds a
private mount root and Landlock, and therefore requires Landlock ABI 5 or
newer. Hosts must permit unprivileged user namespaces. A missing prerequisite
rejects the entry or launch; it never runs the target without the requested
policy.

When `fs` is omitted, the ordinary host filesystem remains visible except for
kernel-control surfaces. The target receives a PID-namespace procfs with no
host sysctls, and `/sys/fs/cgroup` is masked so it cannot change a delegated
limit or migrate into the runtime cgroup. The supervisor is non-dumpable, and
the target enters exec with locked securebits and empty effective, permitted,
inheritable, ambient and bounding capability sets. Unexpected procfs or
cgroupfs mounts outside their standard trees reject the launch.

The current filesystem backend accepts existing directory grants. Individual
file grants are valid policy but return `CONFINE_UNSUPPORTED` until a pinned
file-mount path is implemented. An omitted `fs` block is unrestricted. A
launch may narrow that baseline by specifying all three filesystem classes;
a patch that leaves any class unrestricted while restricting another is also
reported as unsupported because the private-root backend cannot represent a
partially unrestricted view. `{home}` and `{tmp}` denote the exact private
directory roots; private descendants do not have a separate placeholder
syntax in this slice.

The restricted view is intentionally narrower than a general POSIX root. It
contains only selected `/dev` nodes and no `/proc`. The target seccomp policy
globally denies chmod, chown, xattr and timestamp mutation even within writable
grants, and denies most ioctls. Tools such as compilers, Git and package managers may need
more semantics than `fs.write` currently supplies. Dynamically linked programs
also need read and execute grants for their loader and shared-library trees.

`home: private` works with or without a restricted filesystem. Without `fs`,
it overlays a runtime-created mountpoint with a launch-private mode-0700 tmpfs;
the host sees no home contents, and the mount disappears with the namespace
tree. It does not imply that other host paths are unreadable. With `fs`, add
`{home}` to the desired access classes. TMPDIR is generated only when `{tmp}`
is present in the filesystem policy.

`limits.mem_mb` and `limits.pids` additionally need a delegated cgroup v2
layout with an empty parent and a `runtime` leaf containing the Wippy process.
The launcher creates per-process cgroups as siblings of that leaf, enables only
the requested controllers, and checks actual write access and membership
before releasing the helper. On systemd versions that support it, the service
configuration is:

```ini
[Service]
Delegate=yes
DelegateSubgroup=runtime
```

An equivalent non-systemd supervisor may create the same layout. Merely
setting `Delegate=yes` while Wippy remains directly in the populated service
cgroup is insufficient: cgroup v2 cannot enable domain controllers below a
populated parent. The runtime does not move itself into an arbitrary cgroup or
fall back to per-process `rlimit` for a stated whole-tree limit.

`process_group` is currently rejected for confined launches. The target still
runs below a PID-namespace supervisor and PTY launches still receive their
foreground process group, but arbitrary group signaling needs a supervisor
control channel before it can be implemented without a recycled numeric PGID.

`network: none` is total socket denial, including local `socketpair`; it does
not provide loopback or a broker/proxy exception. Linux always tears down the
remaining PID-namespace descendants after the target exits and on normal
runtime Stop/owner cleanup. `tree.kill_on_owner_exit: false` means only that
the portable minimum was not requested; it does not weaken this backend or
request descendant survival. Runtime-crash cleanup is not guaranteed.

The backend does not currently provide CPU or I/O quotas, persistent private
home contents, launch-time policy widening, or a network egress proxy. Existing
hardlinks inside an admitted directory are admitted objects. READY proves that
the restrictions were installed, not that the target survived the following
exec interval.

The `WIPPY_REQUIRE_CONFINEMENT_CGROUP=1` test setting turns an unavailable
delegation into a test failure; CI claiming positive cgroup coverage must set
it on a runner with the layout above.
