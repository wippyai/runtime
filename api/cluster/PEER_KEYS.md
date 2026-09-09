# Native peer key sources

A compiled host can approve new transport identities without restarting its
cluster. `PeerKeySource` is a concurrent, prompt lookup of host-approved public
keys. Return an owned key copy and `true`, or `false` on missing approval or any
lookup failure. Enrollment, persistence and approval policy belong to the host.

For direct assembly, set `cluster.StackConfig.InternodePeerKeySource`. For the
standard boot component, supply a typed `cluster.PeerKeySource` as the native
config value `cluster.internode.peer_key_source`, for example through a native
application launch plan. YAML strings, maps and nil functions are rejected.
There is no Lua permission or registry metadata that installs this callback.

Static `internode.trusted_peer_keys` pins take precedence. The local node still
requires its configured pin to match its private signing key. For a remote node,
runtime requires both an approved key and a matching membership advertisement;
the existing handshake then proves possession of that key. Gossip cannot create
approval, overwrite a pin or authenticate a claimed identity by itself.

The source is consulted for subsequent handshakes. Removing approval does not
disconnect established peers or revoke their application grants. Transport and
application owners must retire those resources separately. Key rotation of a
statically pinned identity requires changing its pin. The source adds no listener,
certificate format, enrollment protocol or background polling task.

The runtime tests cover a running stack rejecting an unknown client and then
connecting after host approval without rebinding the owner's listener. They also
cover static-pin precedence, unapproved or mismatched gossip keys, copied values,
and invalid boot configuration. This is transport identity admission, not
authorization to open a terminal, edit an application or administer a workspace.
