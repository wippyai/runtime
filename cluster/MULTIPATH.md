# Mesh paths

The runtime advertises listener candidates in additive membership metadata:
`internode_candidates_v3` and `gossip_candidates_v3`. Each value is a versioned,
bounded list of host, port, address family, and scope. The existing membership
address, `internode_port`, and optional `internode_advertise_addr` / `_port`
remain for older peers. Memberlist's 512 byte metadata limit determines how
many candidates fit; the publisher keeps the largest prefix that fits. Explicit
candidates come first, followed by active non-loopback interfaces, with tailnet
and public interfaces ahead of bridge interfaces. Loopback and link-local
addresses are never advertised as remote candidates.

For the boot component, `cluster.membership.advertise_candidates` and
`cluster.internode.advertise_candidates` are comma-separated `host` or
`host:port` values. Use `[IPv6]:port` for an IPv6 port override. Hosts without a
port use the live listener port. A forwarded or relay port can be listed with
its external port. `cluster.membership.advertise_addr` and
`cluster.internode.advertise_addr` still select the legacy paths. Native
`cluster.StackConfig` uses `MembershipAdvertiseCandidates []string` and
`InternodeAdvertiseCandidates []internode.Candidate`.

For known members, UDP gossip is sent to the bounded candidate pool, and TCP
membership dials race its paths. Internode dials stagger candidates by 25 ms,
complete the existing authenticated handshake, and select only the first
identity-verified connection. If mutual TLS is configured, the TLS handshake
also runs on every candidate. The last verified path leads the next reconnect
race, while the current candidate pool and a learned listener address remain
available. Retries continue with capped exponential delay while the peer is
managed. Candidate metadata and observed addresses grant no trust; the
existing pinned Ed25519 key and host admission still decide membership.

`system.cluster.members()` includes `mesh` for each managed remote member:
`state`, `direction`, `path`, `local_address`, `remote_address`, `observed`, and
`candidates`. `path` is the authenticated live internode endpoint and is empty
when disconnected. `observed` is the peer socket address, which may contain an
ephemeral source port. Native callers can use `internode.Service.PeerStatus` or
`MeshPeerStatusSource` from `api/cluster`.

Bee should pass its authenticated joiner's candidate list to the inviter over
the existing pinned join channel and return the inviter's candidate list in
the join response. If inviter A cannot reach joiner B, A requests a reverse
dial in that response (or a later authenticated supervisor session). After B
starts and admits A as a mesh member, B calls
`system.GetInternodeService(ctx).RequestReverseConnect(inviterID, inviterCandidates)`.
The reverse dial uses the same TLS and identity checks and can connect even
when node ID order would normally make B wait for inbound traffic. Bee should display
`system.cluster.members()[i].mesh.path` in `bee hive peers`, use `mesh.state`
for transport state, and retain `mesh.candidates` for diagnostics. Its invite
and join seed should carry reachable gossip candidates too. An offline inviter
that cannot receive a request still needs an operator-provided route or relay;
WSL2 NAT and Docker bridge addresses require forwarding or a reachable
Tailscale/LAN path.
