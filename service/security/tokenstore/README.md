# Token validation authority

A token store can declare a `subject_lookup` function:

```yaml
kind: security.token_store
store: app:token_storage
subject_lookup: app:subject_lookup
```

Validation checks the bearer signature and backing-store entry before calling the
function. Backing-store expiration and revocation retain their existing behavior.
The function receives one argument:

```lua
{subject_id = "stored-actor-id", authority_only = true, actor_meta = {}, token_meta = {}}
```

The actor ID comes from the authenticated stored token. Metadata comes from that
same entry and lets the host identify explicit credential restrictions. The
function runs through the function registry with its declared security identity.
It must check that the subject exists and is active, and return the union of the
subject's current authority:

```lua
{
    subject_id = "stored-actor-id",
    meta = {},
    groups = {"app:current_group"},
    policies = {"app:read"}
}
```

`policies` optionally lists additional current policy IDs selected by the host.
The `groups` and `policies` fields use the same names and registry-ID lists as
Runtime's security configuration and the credential ceiling below. Runtime
resolves each policy ID directly from the policy registry and unions its policy
with the group policies. Unknown or malformed policy IDs fail validation without
returning partial authority. Omitting `policies` preserves group-only authority.
These are live selections, not the stored issuance policy list.

`groups` lists current memberships in native Security groups. Issuance scope
labels and default scopes do not supply membership authority. `authority_only`
asks the host for current authority without materializing a login scope. `groups`
must be present, but may be empty when authority comes from `policies` alone.
When both lists are empty, the subject has no authority. Runtime obtains current
policies on every validation. The returned subject ID must match the stored
actor ID.
Returned metadata replaces issuance metadata; stored groups and policy IDs do
not supply authority. Native validation and HTTP header, query, cookie and
websocket-upgrade admission use this same path.

When a credential carries explicit narrowing, the host resolves that stored
restriction and returns an optional `ceiling = {groups = {}, policies = {}}`.
Authority and ceiling must both allow a request; either deny takes precedence.
An absent ceiling leaves current authority unchanged. An empty ceiling
grants no access. The host must return an error when a stored restriction cannot
be resolved, rather than omit the ceiling. Issuance-time `scope_policies` is an
access snapshot, not an explicit credential ceiling.

Credential ceilings are enforced in native scopes and remain attached to copied
policies. The current Temporal security envelope and token issuance format carry
policy IDs only, so they cannot represent these constraints. Propagating such a
scope through Temporal or minting another token directly from it returns an
explicit non-retryable error instead of silently discarding the ceiling. Ordinary
policy-ID scopes retain their existing formats and behavior.

The function can return a runtime error or a structured failure:

```lua
{success = false, error = {kind = "PermissionDenied", message = "subject is inactive"}, retriable = false}
```

Errors propagate to native callers. Missing or malformed authority, unavailable
groups and unavailable ceiling policies fail validation. HTTP admission retains
its existing unauthenticated-request behavior on validation failure.

Without `subject_lookup`, actor metadata and policy-ID reconstruction retain
their existing behavior. The hook also applies to existing stored tokens; no
migration or token rewrite occurs. Enabling it intentionally changes a token
from issuance-snapshot authority to current subject authority, subject to any
explicit credential ceiling supplied by the host.
