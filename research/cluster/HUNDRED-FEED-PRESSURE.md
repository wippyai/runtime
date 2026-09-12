# 100 live participant feeds: bounded authority pressure

Command:

```sh
GOWORK=off go test -race ./system/topology/namereg/kvbacked -run '^TestParticipantHundredFeedsRecoverUnderBoundedAuthority$' -count=1 -v
```

Log: `/home/wolfy-j/wippy/participant-hundred-feeds-20260911.log`.

Fixture: one standalone authoritative KV service, 100 forwarding participant
endpoints with independent local KV services, 1000 Consistent claims, simulated
relay provenance, eight authority captures plus eight busy replies at most.
All nodes/keys are registered in the simulated router before feeds start.
The configured backstop is one hour so explicit coalesced hints control this
short pressure test; it does not measure production idle timers.

After sequential admission, every client receives a burst of 100 refresh hints.
Only clients still missing a completed successful response/readiness are retried.
Then transport is refused, every client must close readiness, transport is
restored, and every client must complete a fresh response and restore readiness.
The committed inventory must retain all 101 participants throughout failure.
Endpoint cleanup is registered after its engine cleanup so LIFO joins endpoint
work before stopping its local engine, including on partial fixture failure.

Final race-enabled sample:

| Measurement | Result |
| --- | --- |
| Warm refresh burst | 808.5ms |
| Recovery after transport restoration | 278.0ms |
| Heap sampled after GC, warm / recovered | 22,201,504 / 22,404,824 bytes |
| Live goroutines, warm / recovered | 307 / 307 |
| Test result | PASS, 3.43s |

Heap/goroutine samples are from a live fixture and may include trailing coalesced
work; they do not prove an idle footprint or a long-term leak bound. An earlier
run before tightening cleanup order measured655ms/202ms, illustrating variability.

This establishes 100 concurrent live naming feeds can recover under bounded
capture/refusal admission in this fixture. It is not 100 OS processes, native
TLS, gossip, Raft, application-message load, or a dormant full-mesh measurement.
Those require separate native cluster evidence and remain outstanding.

## Complementary real Raft / 100-client Strong test


```sh
GOWORK=off go test -race ./cluster/clustertest -run '^TestE2E_StrongIncludesHundredForwardingClients$' -count=1 -v
GOWORK=off go test -race ./system/topology/... ./system/kv ./boot/components/system
```

The 3-Raft-member + 100-forwarding-client test passed in 11.76s (package12.807s),
Strong promotion304.825862ms. It verifies exclusion at all103participants,
partitioned-client readiness closure, Strong refusal naming that participant,
continued Consistent registration, and Strong recovery after healing. Transport
is in-process: this is not native100-node or dormant mesh acceptance.
Log: /home/wolfy-j/wippy/participant-hundred-strong-recheck-20260911.log.
