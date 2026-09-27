# Windows confinement host contract

Windows confined launches use a fresh Less Privileged AppContainer identity
and a Job Object. The target is created suspended, placed in the Job atomically,
and resumed only after the runtime verifies the exact package SID, LPAC token,
empty capability set, low integrity level, and Job membership. Only stdin,
stdout, and stderr are inherited. The Job supplies aggregate committed-memory
limits, wall-time termination, and whole-tree cleanup.

LPAC identities do not inherit ordinary user filesystem access. The runtime
therefore adds the launch's unique package SID to the bound working directory,
private home, and executable DACLs for the launch, then removes that SID after
the Job is empty. Object handles are retained without delete sharing so path
replacement cannot redirect creation or cleanup. A named mutex derived from
the volume/file identity serializes DACL read-modify-write across Wippy runtime
processes. NULL DACLs are rejected, and cleanup errors are returned by Wait.

This mechanism assumes Wippy controls ACL management for configured working
directory trees while launches are active. Windows does not provide a
compare-and-swap DACL update, so an unrelated external ACL writer that ignores
Wippy's object mutex can still race the standard GetSecurityInfo/SetSecurityInfo
sequence. Inherited ACE cleanup is rooted at the retained directory handle;
host software must not move descendants out of the controlled tree or protect
their inheritance during a launch. Use dedicated runtime-owned work roots, not
shared administration trees. A requested launch fails closed when these access
changes cannot be applied or reverted.

The backend currently supports environment ceilings, private home, aggregate
Job memory, wall time, and tree cleanup. Filesystem policy blocks, total socket
denial, portable task-count limits, confined PTYs, and confined process groups
return `CONFINE_UNSUPPORTED`; they are not silently weakened.

The runtime resolves and supplies trusted `SYSTEMROOT` and `LOCALAPPDATA`
bootstrap values required by Windows process and AppContainer creation. Windows
then rewrites `LOCALAPPDATA`, `TEMP`, and `TMP` into the package-private profile.
Those four names are platform-managed and cannot be set or admitted by an entry
environment policy. No other host environment is implicitly inherited.
