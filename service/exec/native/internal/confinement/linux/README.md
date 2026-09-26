# Linux confinement host setup

Every confined launch uses user, mount, PID and IPC namespaces, a minimal
verified helper, `no_new_privs`, and seccomp. `network: none` adds a fresh
network namespace and denies socket operations. A restricted `fs` view adds a
private mount root and Landlock, and therefore requires Landlock ABI 5 or
newer. Hosts must permit unprivileged user namespaces. A missing prerequisite
rejects the entry or launch; it never runs the target without the requested
policy.

The current filesystem backend accepts existing directory grants. Individual
file grants are valid policy but return `CONFINE_UNSUPPORTED` until a pinned
file-mount path is implemented. An omitted `fs` block is unrestricted. A
launch may narrow that baseline by specifying all three filesystem classes;
a patch that leaves any class unrestricted while restricting another is also
reported as unsupported because the private-root backend cannot represent a
partially unrestricted view. `{home}` and `{tmp}` denote the exact private
directory roots; private descendants do not have a separate placeholder
syntax in this slice.

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

The `WIPPY_REQUIRE_CONFINEMENT_CGROUP=1` test setting turns an unavailable
delegation into a test failure; CI claiming positive cgroup coverage must set
it on a runner with the layout above.
