# Native snapshot load measurement — 2026-09-11

Command (integration checkout, GOWORK disabled):

```sh
WIPPY_PARTICIPANT_LOAD_DURATION=30s GOWORK=off go test -race ./system/topology/namereg/kvbacked -run '^TestParticipantIndependentTLSProcesses$' -count=1 -v
```

Log: `/home/wolfy-j/wippy/participant-native-load-20260911.log`.

The optional load phase defaults to 1,000 committed Consistent claim records.
WIPPY_PARTICIPANT_LOAD_CLAIMS controls dataset size (0–10,000). The normal test has
no timed load phase and retains its 512-byte/1ms fragmented proxy. Load mode uses
16KiB proxy forwarding buffers without artificial delay. TLS is end to end;
separate node processes have pinned signing identities and a shared mesh key.
Only the parent harness uses stdio commands. All naming bytes traverse TCP.

Observed client-side values:

| Measurement | Result |
| --- | --- |
| Completed full refreshes | 1,057 |
| Load phase duration | 30.0046 seconds |
| Busy replies | 0 |
| Client allocations during phase | 1,912,236,568 bytes |
| Client live heap before / after GC | 1,133,448 / 1,162,424 bytes |
| Client goroutines before / after | 13 / 13 |
| Complete fixture including faults/shutdown | PASS, 45.84 seconds |

About 35.2 complete refreshes/second and 1.81MB allocated/client refresh. This is
race-instrumented, single-client snapshot processing, not raw transport throughput
or cluster capacity. Allocation includes client background work and validation;
no authority-side CPU/allocation or network-byte metric is reported. Stable live
heap/goroutine samples are encouraging but do not establish a long-term leak bound.

After the load phase the same processes passed stalled-TCP refresh failure and
recovery, cut/refused-connection failure and automatic recovery, exact retirement
refusal, and joined shutdown. This still uses standalone KV engines, not a native
Raft group or a production boot configuration. Authority retirement is explicitly
commanded by the fixture after the client seals admission.

Next performance target: avoid sending and repeatedly decoding full unchanged
snapshots. Preserve a linearizable authority check, exact incarnation enrollment,
revision monotonicity, bounded cached state, and complete exclusion installation;
a fast local revision read alone cannot authorize an unchanged response.

## Bounded conditional client cache measurement

Same command, race instrumentation, 30s duration and 1000 claims after enabling
conditional authority replies and a one-snapshot client cache:

| Measurement | Cached candidate |
| --- | --- |
| Completed full local refreshes | 3,072 |
| Load phase duration | 30.0094 seconds |
| Busy replies | 0 |
| Client allocations during phase | 4,576,598,288 bytes |
| Client live heap before / after GC | 1,282,008 / 1,310,984 bytes |
| Client goroutines before / after | 13 / 13 |
| Complete fixture including faults/shutdown | PASS, 45.57 seconds |

Log: `/home/wolfy-j/wippy/participant-native-cached-load-20260911.log`.

Approximately 102 refreshes/s vs 35, and 1.49MB/client refresh vs 1.81MB. The
higher total allocation is from completing ~2.9x as many refreshes; per-refresh
allocation fell about 18%. These are short, sequential, instrumented samples,
not controlled cluster capacity or raw mesh throughput. No authority resource or
wire-byte counters are included. Subsequent lifetime/revision rejection guard
and tests do not change the successful unchanged path.

Each successful refresh still validates entries and reconciles local obligations.
Unchanged does not bypass exclusion cleanup or identity checks. The client keeps
one decoded snapshot; visitor-visible value bytes are cloned. Heap baseline grew
about 149KB compared with the uncached run, consistent with retained cache cost;
one before/after sample cannot establish an exact bound or absence of all leaks.

## Reuse validated exclusions during local reconciliation

Snapshot capture now retains only validated Strong active/pending metadata for
its immediate reconciliation pass. Consistent records remain fully validated,
but refresh no longer decodes every record again merely to discard Consistent
entries. Pending attestation, bootstrap exclusion installation, revision checks
and generation-aware terminal cleanup remain in place.

Same 30s/1000-claim race-instrumented native workload:

- 4,566 refreshes, zero busy, 30.0008s.
- Client allocations: 4,594,146,376 bytes (~1.01MB/refresh).
- Client heap after GC: 1,288,904 -> 1,382,312 bytes; goroutines 13 -> 13.
- Entire load/fault/recovery/refusal/shutdown fixture passed in 45.55s.

Log: `/home/wolfy-j/wippy/participant-native-single-decode-load-20260911.log`.
This is ~1.49x the cached candidate's completion count and ~4.32x the initial
uncached sample, with ~33% lower allocation/refresh than cache-only (~44% lower
than initial). Short sequential samples; real Raft regression ran concurrently
for part of this sample. No raw mesh, 100-client, authority CPU, or long-term leak
claim follows from these figures. Retained heap increased ~93KB in this sample.
