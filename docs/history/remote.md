# Remote registry history

Use `wippy run --set registry.history_type=grpc` to select the remote service. Set each option with a separate `--set` argument.

History uses the default Wippy credential. Set `WIPPY_TOKEN` or use the credential saved by `wippy auth login`. The runtime uses the same credential selection order as Hub: runtime override, environment, local login, then global login. The History server must authorize that credential for the selected registry.

For Stage, put these settings in `.wippy.yaml`:

```yaml
version: "1.0"
registry:
  history_type: grpc
  history_endpoint: history.stage.wippy.ai:443
  history_tenant_id: <organization-id>
  history_environment_id: stage
  history_registry_id: <registry-id>
```

Then run `wippy run`. A separate History token file and a replica ID are optional.

| Option | Source |
| --- | --- |
| `registry.history_endpoint` | Required service address. |
| `registry.history_tenant_id` | Required tenant identity. |
| `registry.history_environment_id` | Required environment identity. |
| `registry.history_registry_id` | Required registry identity. |
| `registry.history_replica_id` | Optional replica identity. The runtime generates a unique ID for each History connection. |
| `registry.history_token_file` | Optional credential override. An unreadable or empty file fails without falling back to the default credential. |
| `registry.history_ca_file` | Service CA file. The system trust store applies when this option is empty. |
| `registry.history_server_name` | TLS server name. The connection target supplies the name when this option is empty. |
| `registry.history_cert_file` | Optional client certificate for mutual TLS. |
| `registry.history_key_file` | Client key. Set this option with the client certificate. |
| `registry.history_timeout` | Request timeout. The default is 15s. An explicit value must be positive. |
| `registry.history_poll_interval` | Receipt poll interval. The default is 100ms. An explicit value must be positive. |
| `registry.history_max_message_bytes` | Message limit. The default is the gRPC Go receive limit. Set it to the measured service limit. |

The gRPC Go receive default is 4 MiB. See the [gRPC Go source](https://github.com/grpc/grpc-go/blob/v1.83.2/clientconn.go). The entry codec uses the same upper limit. MessagePack supplies the decoder depth and initial allocation limits. See the [MessagePack decoder source](https://github.com/hashicorp/go-msgpack/blob/v2.1.5/codec/decode.go). Stream reconnection uses the [gRPC backoff configuration](https://github.com/grpc/grpc-go/blob/v1.83.2/backoff/backoff.go). These library defaults are protocol limits. They are not measured capacity targets.

A registry snapshot must fit in one response. Measure its protobuf size before migration. Configure the client and service to use the same tested limit. The service can store many separate registries. This protocol does not split one snapshot into message chunks.

The generated replica ID stays fixed for the lifetime of the History connection. Set `registry.history_replica_id` when an operator requires the same identity across restarts, such as for an explicit replica acknowledgement list. The service uses this identity for apply reports. Each submission and restore request includes the revision of the applied state. The service rejects the request if the stored revision changed. The runtime returns this response as a conflict. It does not retry the planned change with a newer revision. If a commit result is unknown, retry the same operation. The runtime keeps the same request ID, expected revision, mutations, and resolution. A confirmed write or a successfully applied publication advances the expected revision.

The runtime confirms local application only after the service publishes a version. A stored receipt can remain pending while Temporal is unavailable. A conflict or rejected graph leaves the last applied version active. The client resolves a lost commit response with the original request ID. It retains an unknown request for a retry with the same data.

The first native publication stores the local baseline. Later startup uses only the published snapshot. A different local baseline does not change that snapshot. Submissions include expanded module entries and the exact dependency graph. Stored entry values and deletion records take precedence over artifact defaults.

An imported snapshot must contain each entry required by its stored graph, or an explicit deletion record. The runtime rejects a snapshot if dependency reconciliation needs an entry that the snapshot does not contain. It reports the application error and retains the previous local version. Legacy histories that omit derived module entries require durable materialization before migration can complete.

The runtime loads a full published snapshot after a restart. It does not require a local registry database. A missing lockfile does not prevent the remote read. A stored deployment graph lets the existing dependency loader retrieve the exact module artifacts. Local overlays remain process-local.

`ApplyVersion` submits a restore change. It does not move the service head to an old version. Legacy reads support exact root versions, imported branches, and original entries. Imported versions retain the original operation order and updates that do not change a value. A cached baseline decoder restores released ownership metadata. Native versions return the effective changes between snapshots. Version enumeration reads metadata pages. It uses memory in proportion to the number of versions. Publication and startup do not enumerate version history.

Export the immutable deployment baseline before migration:

```sh
wippy registry export-history-baseline --lock-file wippy.lock > baseline.json
```

Use the same `--profile` and `--set` options as the deployed runtime. The export uses the existing module entry loader. It preserves entry ownership and root metadata. If the baseline has authored dependency roots, supply `--resolution-file resolution.json`. This file must contain the exact `DependencyResolution` JSON for that baseline. The exporter checks the declarations and graph digest. It does not select new module versions. The output is a Version protobuf JSON document with revision zero. The importer must verify it against the source history and the configured size limit.

Export a raw source bundle with the history service command. Then create complete snapshots with the runtime command:

```sh
wippy registry materialize-history --source source.jsonl --output snapshots.jsonl --max-record-bytes "$HISTORY_RECORD_LIMIT"
```

Set the record limit to the tested service import limit. The bundle contains the immutable baseline. The runtime replays each original transaction from its stored parent. It uses the exact stored graph and verified module artifacts. It preserves branches, authored operations, and deletion records. Root zero can use the baseline graph. A missing graph at a later version stops export when dependency operations or declarations need it. The command does not select a replacement graph.

The reader keeps version identities in memory. The command stores complete branch snapshots and artifacts in a temporary directory beside the output. It removes this directory when export ends. The output replaces its destination only after all records pass validation. Import the completed bundle with the history service command. The service must fence the source and verify that its raw records have not changed. Root-zero snapshots have no legacy changeset. The separate raw root record remains unchanged.

Run remote recovery checks with the `historyintegration` build tag. This check requires an actual service, PostgreSQL, and Temporal. Set the `WIPPY_HISTORY_RECOVERY_*` variables from the test service configuration. The check starts independent writer and reader processes in separate empty directories. Test fixture deadlines and poll intervals are not deployment defaults.
