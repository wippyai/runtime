# Remote registry history

The remote history is a registry history driver on the Wippy History service. It has the same semantics as the memory, SQLite, and PostgreSQL drivers. The registry plans, validates, applies, and rolls back changes in the same way for all drivers.

Each call returns after the service commits or rejects the operation. A save with a head update succeeds only when the head is still the parent version. A conditional head move fails when the head changed. Each version keeps its exact dependency resolution. The history also stores the deployment baseline, so a runtime without project sources can recover the same state.

## Select remote history in a running application

Use the Lua `registry` module:

```lua
local hub = require("hub")
local registry = require("registry")

hub.auth.authenticate(token)
local binding, err = registry.use_remote_history({ name = "my-app" })
```

`use_remote_history` transfers the active local history to the remote history `my-app`, and then switches the registry to it. It copies all versions, branches, dependency resolutions, the head, and the baseline. No registry operation runs during the transfer. The call returns after the service confirms the same content that the runtime read.

The remote history must be empty. The service rejects a history that has other content. If the transfer fails or stops, the local history stays active and unchanged. Call `use_remote_history` again with the same name. The runtime resumes the same transfer and does not duplicate versions. After the switch, all registry writes go to the remote history only. There are no dual writes and no fallback to local writes.

`registry.history_backend()` returns the selected backend. The permissions are `registry.history.get` and `registry.history.select`.

Options:

| Option | Use |
| --- | --- |
| `name` | Required remote history name. |
| `organization` | Organization name. Defaults to the project organization, or to the only organization of the credential. |
| `organization_id` | Organization ID. Bypasses organization lookup. Cannot be combined with `organization`. |
| `environment` | Environment name. Defaults to the first domain label after `hub.` in the Hub URL. |
| `endpoint` | Service address. Defaults to the Hub host with `history.` in place of `hub.`. |

## Selection after restart

The runtime stores the selection in `.wippy/history.yaml`. The file contains the Hub URL, the service address, the organization, the environment, the history name, and the transfer ID. It contains no credentials. At boot, the runtime opens the stored remote history with the credential for that Hub from the Wippy credential store. If the credential is missing, boot stops. It does not fall back to local history.

The credential store is the same store that `wippy auth login` uses: runtime token, `WIPPY_TOKEN`, project login, then global login. If the switch used a token that exists only in the process, such as a token from `hub.auth.authenticate`, the runtime saves it in the project credential store.

## Recover on another runtime

A cloud runtime can open the same history with configuration. Set `WIPPY_TOKEN`, and put these settings in `.wippy.yaml`:

```yaml
version: "1.0"
registry:
  history_registry_id: my-app
```

When `history_registry_id` is set, the history type defaults to `remote`. Without project sources, the runtime loads the stored baseline and replays the history to its head. With project sources, the runtime uses the sources as the baseline and stores it when its digest changed.

| Option | Use |
| --- | --- |
| `registry.history_registry_id` | Remote history name. |
| `registry.history_organization` | Organization name. |
| `registry.history_tenant_id` | Organization ID. Bypasses organization lookup. Cannot be combined with `history_organization`. |
| `registry.history_environment_id` | Environment name. |
| `registry.history_endpoint` | Service address. |
| `registry.history_token_file` | Credential file. An unreadable or empty file fails without falling back to the default credential. |
| `registry.history_ca_file` | Service CA file. The system trust store applies when this option is empty. |
| `registry.history_server_name` | TLS server name. |
| `registry.history_cert_file` | Client certificate for mutual TLS. |
| `registry.history_key_file` | Client key. Set it with the client certificate. |
| `registry.history_timeout` | Request timeout. The default is 15s. |
| `registry.history_max_message_bytes` | Message limit. The default is the gRPC Go receive limit of 4 MiB. Use the same limit as the service. |

## Uncertain results

When a request fails with an unknown result, the driver sends it again with a retry flag. The service then accepts a save or head move that it already applied. It does not accept a different change. A read is sent again only when the service was unavailable.

## Limits

One version change set, one baseline, and one transfer batch must fit the message limit. The transfer sends at most 500 versions or half of the message limit in one batch.
