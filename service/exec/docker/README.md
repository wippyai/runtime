# Docker executor ownership labels

An `exec.docker` entry can select creation-time labels from explicit per-process
environment values:

```yaml
labels_from_env:
  example.owner: CONTAINER_OWNER
  example.attempt: ATTEMPT_ID
```

`NewProcess` freezes the mapping and values before creating the container.
Missing, empty, oversized or NUL-containing values refuse process creation.
Sources come from the explicit process environment, not ambient environment or
executor defaults. Select only nonsecret identities: Docker labels are visible
to daemon clients. The mapping has at most 64 entries; each label key is at most
256 bytes and each value at most 4096 bytes. Keys must be nonempty and contain
no NUL. Source names must be nonempty and contain neither `=` nor NUL.
Invalid mappings and source values return the canonical `Invalid` error kind
with retry disabled. Errors identify the source name, never its value.

Labels reach `containers/create` for both streamed and PTY processes, including
when the daemon returns an error or the request exceeds its deadline. A failed
create does not prove absence: an orchestrator can reconcile its exact labels
after a delayed daemon completion. Labels describe ownership and never grant
permission to use the executor or operate on containers.
