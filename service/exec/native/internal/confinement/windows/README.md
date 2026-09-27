# Windows confinement host contract

Windows confined launches use a fresh Less Privileged AppContainer identity
and a Job Object. The target is created suspended, placed in the Job atomically,
and resumed only after the runtime verifies the exact package SID, LPAC token,
single `internetClient` capability, low integrity level, and Job membership.
Only stdin, stdout, and stderr are inherited. A token-level child-process
restriction and a Job active-process limit make the sandbox an explicit
singleton. The Job supplies committed-memory limits, wall-time termination,
and owner-exit cleanup.

Windows currently rejects `network: none`, so every supported launch does not
request network denial. It receives exactly the outbound-public-Internet
`internetClient` capability; the runtime checks the target token's capability
SID, count, and attributes before resume. This is not inherited host
networking and does not promise inbound, private-network, or loopback access.
No shared filesystem or user-data capability is granted.

LPAC status is verified by an in-memory `AccessCheck` that must grant the
`ALL RESTRICTED APPLICATION PACKAGES` bit while withholding the ordinary
`ALL APPLICATION PACKAGES` bit. This proves the effective access semantics
without relying on the inconsistently supported LPAC token-information class.

LPAC identities do not inherit ordinary user filesystem access. The runtime
therefore adds the launch's unique package SID to the bound working directory
and executable DACLs for the launch, then removes that SID after the Job is
empty. Private homes use the package-private profile directory created by
Windows; the runtime gives that launch-owned directory the same inheritable
low-integrity label and unique package-SID access Chromium applies to its
AppContainer profiles. Object handles are retained without delete sharing so
path replacement cannot redirect creation or cleanup. A named mutex derived
from the volume/file identity serializes DACL read-modify-write across Wippy
runtime processes. NULL DACLs are rejected, and cleanup errors are returned by
Wait.

Writable work directories must be provisioned with an inheritable low
mandatory-integrity `NO_WRITE_UP` label. The runtime validates this and fails
closed; it does not temporarily lower an arbitrary host tree's object-wide
label because doing so races other runtimes and changes access for unrelated
low-integrity processes. The unique package-SID DACL remains the per-launch
AppContainer restriction. Never grant `ALL RESTRICTED APPLICATION PACKAGES`.
Provision a dedicated root from an administrative shell with, for example,
`icacls C:\wippy-work /setintegritylevel "(OI)(CI)L"`.

This mechanism assumes Wippy controls ACL management for configured working
directory trees while launches are active. Windows does not provide a
compare-and-swap DACL update, so an unrelated external ACL writer that ignores
Wippy's object mutex can still race the standard GetSecurityInfo/SetSecurityInfo
sequence. Inherited ACE cleanup is rooted at the retained directory handle;
host software must not move descendants out of the controlled tree or protect
their inheritance during a launch. Use dedicated runtime-owned work roots, not
shared administration trees. A requested launch fails closed when these access
changes cannot be applied or reverted.

The backend currently supports environment ceilings, private home, Job memory,
wall time, and owner-exit cleanup in its singleton process domain. Filesystem
policy blocks, total socket denial, portable task-count limits, confined PTYs,
and confined process groups return `CONFINE_UNSUPPORTED`; they are not silently
weakened.

The runtime resolves and supplies trusted `SYSTEMROOT` and `LOCALAPPDATA`
bootstrap values required by Windows process and AppContainer creation. Windows
then rewrites `LOCALAPPDATA`, `TEMP`, and `TMP` into the package-private profile.
Those four names are platform-managed and cannot be set or admitted by an entry
environment policy. No other host environment is implicitly inherited.
