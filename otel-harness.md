# opentelemetry-erlang SDK — Robustness Harness Test Catalog

Target: `opentelemetry-erlang` SDK (API + SDK + OTLP exporter).
Format: `ID | Title | Intent | Expected`. One row per test.
Total tests: **320** across 9 sections.

## Table of Contents
1. [Lifecycle & Supervision (LIFE) — 30](#lifecycle--supervision-life)
2. [Failure Injection / Chaos (CHAOS) — 45](#failure-injection--chaos-chaos)
3. [Load & Throughput (LOAD) — 40](#load--throughput-load)
4. [Concurrency & Scheduler (CONC) — 35](#concurrency--scheduler-conc)
5. [Backpressure & Buffering (BP) — 30](#backpressure--buffering-bp)
6. [Protocol Conformance OTLP (PROTO) — 40](#protocol-conformance-otlp-proto)
7. [Security & Auth (SEC) — 30](#security--auth-sec)
8. [Context Propagation (CTX) — 35](#context-propagation-ctx)
9. [Sampling & Resource Detection (SAMP) — 35](#sampling--resource-detection-samp)

---

## Lifecycle & Supervision (LIFE)

| ID | Title | Intent | Expected |
|---|---|---|---|
| LIFE-001 | Cold app start | Boot `opentelemetry` then `opentelemetry_exporter` | All children `running`, no crash reports |
| LIFE-002 | Reverse stop order | Stop exporter before SDK | Clean shutdown, queued spans flushed |
| LIFE-003 | Supervisor tree shape | Verify `otel_tracer_provider_sup` children | Matches documented shape |
| LIFE-004 | BatchProcessor crash | Kill batch processor mid-run | Restarted within `max_restarts`, spans resumed |
| LIFE-005 | Exporter crash | Kill exporter gen_server | Restart, queue preserved |
| LIFE-006 | Sampler crash isolation | Custom sampler raises | Span dropped, tracer survives |
| LIFE-007 | Restart intensity exceeded | Force >max_restarts in window | Supervisor escalates, parent restarts |
| LIFE-008 | Brutal kill exporter | `exit(Pid, kill)` | Restart, in-flight batch lost only |
| LIFE-009 | Graceful shutdown drain | `application:stop` with full queue | Drain within `otel_span_processor:force_flush` timeout |
| LIFE-010 | Shutdown timeout exceeded | Drain longer than timeout | Returns `{error, timeout}`, remaining dropped |
| LIFE-011 | Hot code reload SDK | `code:load_file(otel_tracer)` under load | No crash, spans continue |
| LIFE-012 | Release upgrade appup | Apply `appup` on running node | State migrated, no span loss |
| LIFE-013 | Release downgrade | Roll back appup | State restored |
| LIFE-014 | Config reload at runtime | Change `processors` env then re-init | New config active, old procs stopped |
| LIFE-015 | ETS recreation after crash | Tracer registry ETS owner dies | Recreated by supervisor |
| LIFE-016 | Persistent term refresh | Update tracer config persistent_term | New tracers see update |
| LIFE-017 | Boot without collector | Start with collector unreachable | App boots, exporter retries |
| LIFE-018 | Boot with bad config | Invalid endpoint URL | Fails fast with clear error |
| LIFE-019 | Multiple tracer providers | Start named providers | Independent lifecycle |
| LIFE-020 | Provider shutdown idempotent | Call `shutdown` twice | Second call no-op |
| LIFE-021 | Force_flush on stopped provider | Flush after shutdown | Returns `{error, already_shutdown}` |
| LIFE-022 | Restart preserves resource | Resource attrs survive restart | Identical resource on new exporter |
| LIFE-023 | Child spec validity | `supervisor:check_childspecs` | All ok |
| LIFE-024 | Application:which_applications | Verify deps loaded | All deps present |
| LIFE-025 | Distributed start across nodes | 3-node cluster boot | Each node independent provider |
| LIFE-026 | rebar3 release boot | Boot from release tarball | Same behavior as dev |
| LIFE-027 | Mix release boot (Elixir interop) | Boot under Elixir release | Functions identically |
| LIFE-028 | sys:get_state safety | Inspect exporter state | No deadlock |
| LIFE-029 | sys:suspend / resume | Suspend exporter, accumulate, resume | Drains backlog |
| LIFE-030 | Supervisor strategy switch | Verify `one_for_one` vs `rest_for_one` semantics | Matches spec |

## Failure Injection / Chaos (CHAOS)

| ID | Title | Intent | Expected |
|---|---|---|---|
| CHAOS-001 | Collector down at boot | Endpoint unreachable | Exporter retries with backoff, no crash |
| CHAOS-002 | Collector down mid-flight | Kill collector after N batches | Retries, spans buffered until queue full |
| CHAOS-003 | DNS resolution fail | Bad endpoint hostname | Periodic re-resolve, log warning |
| CHAOS-004 | DNS slow (5s) | Delayed SRV reply | Honors connect timeout |
| CHAOS-005 | TCP RST mid-send | Toxiproxy reset peer | Retry with backoff |
| CHAOS-006 | Slow loris collector | Trickle bytes back | Request timeout, retry |
| CHAOS-007 | Half-open connection | Drop ACKs | Detect via keepalive, reconnect |
| CHAOS-008 | TLS handshake fail | Cert mismatch | Connection error, no span loss until queue full |
| CHAOS-009 | TLS renegotiation mid-stream | Force renegotiation | Survives or reconnects |
| CHAOS-010 | Partial HTTP write | Connection killed at half body | Retry full batch |
| CHAOS-011 | Disk full file exporter | tmpfs at 100% | `{error, enospc}`, drop with metric |
| CHAOS-012 | Inode exhaustion | No free inodes | Same as disk full |
| CHAOS-013 | OOM exporter proc | Inject huge attrs | Process killed, supervisor restart |
| CHAOS-014 | OOM killed by OS | cgroup memory limit | BEAM survives if possible |
| CHAOS-015 | Scheduler overload | `+SDio 1` and saturate | No deadlock, backpressure engages |
| CHAOS-016 | Port crash (gun/hackney) | Kill linked port | Reconnect |
| CHAOS-017 | Network partition (toxiproxy) | Cut traffic 30s | Reconnect when healed |
| CHAOS-018 | Asymmetric partition | Egress only blocked | Detect via timeouts |
| CHAOS-019 | Clock jump forward | `date -s` +1h | Spans use monotonic; durations correct |
| CHAOS-020 | Clock jump backward | -1h | Negative durations not produced |
| CHAOS-021 | NTP slew | Slow drift | No anomaly |
| CHAOS-022 | SIGTERM during export | Kill BEAM mid-batch | OS reaper waits for `init:stop` |
| CHAOS-023 | SIGKILL BEAM | Hard kill | Best-effort; no corrupt state on next boot |
| CHAOS-024 | atomics module unavailable | Mock OTP <21.2 | Refuses to boot with clear msg |
| CHAOS-025 | crypto application missing | Stop crypto | Exporter refuses TLS, plain still works |
| CHAOS-026 | inets failure | Stop inets if used | Exporter restart, switch transport |
| CHAOS-027 | gRPC GOAWAY frame | Server sends GOAWAY | Reconnect on new conn |
| CHAOS-028 | HTTP/2 settings stress | Tiny `MAX_CONCURRENT_STREAMS` | Stream queueing works |
| CHAOS-029 | Slow Sampler | Sampler sleeps 100ms | Backpressure to span creation, no deadlock |
| CHAOS-030 | Sampler infinite loop | Loop in `should_sample` | Caller killed by timeout? document behavior |
| CHAOS-031 | Resource detector crash | Detector raises on init | Skipped, default resource used |
| CHAOS-032 | Resource detector hangs | Detector blocks 60s | Bounded by detector timeout |
| CHAOS-033 | ETS table deleted externally | `ets:delete` registry | Recreated by owner restart |
| CHAOS-034 | Persistent term flooding | 10k put/sec | Global GC pressure observed; SDK still functions |
| CHAOS-035 | Beam scheduler bind | Bind to single core | Throughput degrades but no crash |
| CHAOS-036 | File handle exhaustion | `ulimit -n 64` | Exporter logs error, retries |
| CHAOS-037 | Ephemeral port exhaustion | Burn ports | Pool reuses; no crash |
| CHAOS-038 | DNS cache poisoning sim | Return wrong IP | TLS cert verify fails safely |
| CHAOS-039 | MITM proxy injection | Transparent proxy alters body | TLS detects (when enabled) |
| CHAOS-040 | Burst followed by silence | 1M spans then idle 10m | Memory returns to baseline |
| CHAOS-041 | Random kill loop | Chaos monkey on all otel pids 60s | App still alive at end |
| CHAOS-042 | Distributed node disconnect | Net split between nodes | Each node continues independently |
| CHAOS-043 | Distributed node rejoin | Heal split | No duplicate exports |
| CHAOS-044 | epmd down | Stop epmd | Local SDK unaffected |
| CHAOS-045 | Logger handler crash | Crash logger handler | SDK does not cascade |

## Load & Throughput (LOAD)

| ID | Title | Intent | Expected |
|---|---|---|---|
| LOAD-001 | 1k spans/sec sustained 5m | Baseline throughput | 0 dropped, p99 export <1s |
| LOAD-002 | 10k spans/sec sustained 5m | Mid-load | <0.1% dropped |
| LOAD-003 | 50k spans/sec sustained 5m | High-load | Documented drop rate |
| LOAD-004 | 100k spans/sec burst 30s | Peak burst | Queue saturates expectedly |
| LOAD-005 | 1M spans burst | Worst-case burst | Drop policy kicks in cleanly |
| LOAD-006 | Soak 24h at 5k spans/sec | Memory leak hunt | RSS plateau within 10% |
| LOAD-007 | Soak 72h | Long soak | No FD/atom growth |
| LOAD-008 | Metric cardinality 1k | Many label sets | Memory bounded |
| LOAD-009 | Metric cardinality 100k | Cardinality explosion | Cardinality limit honored |
| LOAD-010 | Metric cardinality 1M | Pathological | Drops new series, keeps existing |
| LOAD-011 | Counter add @ 500k/s | Hot path metric | Atomics scale linearly |
| LOAD-012 | Histogram record @ 200k/s | Bucket update | No contention crash |
| LOAD-013 | Up/down counter under churn | Frequent ±1 | Correct final value |
| LOAD-014 | Logs flood @ 50k/s | LogRecord throughput | Batched correctly |
| LOAD-015 | Batch size sweep 1..8192 | Find optimum | Smooth curve, no errors |
| LOAD-016 | max_queue_size sweep | Memory vs drop tradeoff | Documented |
| LOAD-017 | scheduled_delay_ms sweep | Latency vs efficiency | Documented |
| LOAD-018 | exporter_timeout sweep | Slow exporter behavior | Retries vs drops as configured |
| LOAD-019 | Multi-tracer 100 tracers | Lib-name diversity | No bottleneck |
| LOAD-020 | Multi-tracer 10k | Extreme | Registry handles |
| LOAD-021 | Parallel meter readers | 4 readers | Each gets full set |
| LOAD-022 | Span attrs 32 each | Standard | Encodes correctly |
| LOAD-023 | Span attrs 128 each | Limit boundary | Truncated/limited per spec |
| LOAD-024 | Span events 128 | Many events | Limit enforced |
| LOAD-025 | Span links 1024 | Many links | Limit enforced |
| LOAD-026 | Large attribute value 64KB | Big string | Truncated to attribute_value_length_limit |
| LOAD-027 | Reduction budget per span | Measure reductions | Within target (<5k reductions) |
| LOAD-028 | GC frequency under load | Observe minor/major GCs | No pathological full sweeps |
| LOAD-029 | CPU utilization @ 50k/s | Per-core load | Within budget |
| LOAD-030 | Memory utilization @ 50k/s | RSS measurement | Within budget |
| LOAD-031 | Network egress @ 50k/s | Bytes/sec | Matches expected with gzip |
| LOAD-032 | Gzip vs identity throughput | Compare | gzip lower bytes, higher CPU |
| LOAD-033 | Concurrent producers 1k | 1k procs each emitting | Linear scale to a point |
| LOAD-034 | Concurrent producers 10k | More procs | Still progresses |
| LOAD-035 | Concurrent producers 100k | Stress | Backpressure clean |
| LOAD-036 | Mixed signals load | Spans+metrics+logs | Each pipeline independent |
| LOAD-037 | Tail latency p999 | Span end latency | <1ms p999 on hot path |
| LOAD-038 | Cold cache start | First N spans | No outlier latency |
| LOAD-039 | Warm-up convergence | Time to steady state | <30s |
| LOAD-040 | Recovery after burst | Drain time after 1M burst | Documented bounded |

## Concurrency & Scheduler (CONC)

| ID | Title | Intent | Expected |
|---|---|---|---|
| CONC-001 | 1k procs concurrent spans | Basic concurrency | All spans recorded |
| CONC-002 | 10k procs concurrent | Scale | All recorded or accounted |
| CONC-003 | 100k procs concurrent | Heavy | Backpressure engages |
| CONC-004 | ETS write contention | Single-table writer | No deadlock |
| CONC-005 | ETS read_concurrency on | Verify flag set on hot tables | flag = true |
| CONC-006 | ETS write_concurrency on | Verify flag | flag = true |
| CONC-007 | atomics counter race | Many writers | No lost updates |
| CONC-008 | counters wrap behavior | Approach 2^64 | Wrap or saturate per spec |
| CONC-009 | seq_trace interaction | seq_trace enabled | No interference with span ctx |
| CONC-010 | Process dictionary ctx | Span ctx in pdict | Cleared at span end |
| CONC-011 | Async cast ordering | gen_server cast spans | Ordering preserved per producer |
| CONC-012 | Async info messages | `!` send patterns | Ctx propagated explicitly |
| CONC-013 | Scheduler bind +sbt | Bind schedulers | Throughput stable |
| CONC-014 | Dirty CPU schedulers | Force dirty work | SDK unaffected |
| CONC-015 | Dirty IO schedulers | Force dirty IO | SDK unaffected |
| CONC-016 | GC pressure huge heap | 1GB heap proc | Span end completes |
| CONC-017 | Hibernating producer | Span across hibernate | Resumes correctly |
| CONC-018 | Monitor leaks | Long run check | `process_info(monitors)` bounded |
| CONC-019 | Link leaks | Long run check | Linked count bounded |
| CONC-020 | Trapping exit exporter | `process_flag(trap_exit, true)` | Handles EXIT cleanly |
| CONC-021 | spawn_opt fullsweep_after | Various values | No anomaly |
| CONC-022 | priority high producer | Producer at high prio | SDK not starved |
| CONC-023 | priority low exporter | Exporter at low prio | Still progresses |
| CONC-024 | Reductions per span end | Measure | Within budget |
| CONC-025 | Reductions per attribute | Measure | Within budget |
| CONC-026 | Tracing lib in NIF caller | Span around NIF call | Works without dirty interaction issues |
| CONC-027 | Port driver caller | Span around port_call | Works |
| CONC-028 | spawn_link to exporter (anti) | Verify SDK not linking user procs | No unexpected links |
| CONC-029 | Massive mailbox producer | 1M msg backlog | Span end still fast |
| CONC-030 | Erlang scheduler 1 | `+S 1:1` | Functions, slower |
| CONC-031 | Erlang scheduler 64 | `+S 64:64` | Linear-ish scale |
| CONC-032 | persistent_term contention | Frequent reads | Lock-free read scales |
| CONC-033 | persistent_term updates | 1/s updates | Acceptable global GC |
| CONC-034 | Tracer span_processor list mutate | Add/remove processor live | Existing spans unaffected |
| CONC-035 | Race: span_end during shutdown | Concurrent end + shutdown | No crash, span recorded or accounted |

## Backpressure & Buffering (BP)

| ID | Title | Intent | Expected |
|---|---|---|---|
| BP-001 | Queue full drop_oldest | Simple processor drop policy | Oldest dropped, metric incremented |
| BP-002 | Queue full drop_newest | Alt policy | Newest dropped if configured |
| BP-003 | Queue full block | Blocking mode | Caller blocks, no loss |
| BP-004 | Exporter slower than producer | 2x mismatch | Stable drop rate |
| BP-005 | Exporter 10x slower | Severe mismatch | Heavy drops, no OOM |
| BP-006 | Retry queue growth bound | Force retries | Bounded by max_queue_size |
| BP-007 | Batch flush by size | Size threshold | Flushes immediately |
| BP-008 | Batch flush by time | Time threshold | Flushes after delay |
| BP-009 | Batch flush by max_export_batch_size | Max chunk | Splits across requests |
| BP-010 | force_flush returns ok | All flushed | `ok` |
| BP-011 | force_flush returns timeout | Slow exporter | `{error, timeout}` |
| BP-012 | force_flush during shutdown | Race | Either ok or already_shutdown |
| BP-013 | Shutdown flush | Drain on stop | All in-queue spans exported |
| BP-014 | Restart preserves pending | Exporter restart with batch | Batch retried |
| BP-015 | Idempotent retry | Same batch twice | Collector dedupes by trace/span id |
| BP-016 | Retry honors Retry-After | Server hint | Backoff matches |
| BP-017 | Retry exponential backoff | No hint | 2^n with jitter |
| BP-018 | Max retry attempts | After N retries | Drop with metric |
| BP-019 | Drop counter metric | Verify SDK self-metrics | Increments correctly |
| BP-020 | Queue length self-metric | Observe queue depth | Reported |
| BP-021 | Export latency self-metric | Histogram per export | Reported |
| BP-022 | Backoff cap | Max delay enforced | <=cap |
| BP-023 | Multiple processors backpressure | Two batch procs | Independent |
| BP-024 | Simple processor blocking | SimpleSpanProcessor sync | Caller blocks until exported |
| BP-025 | Mixed simple + batch | Both attached | Both receive each span |
| BP-026 | Batch larger than max msg | OTLP 4MB cap | Auto-split to sub-batches |
| BP-027 | Tail flush on idle | Idle 5x scheduled_delay | All spans exported within window |
| BP-028 | Producer faster than gzip | Compression CPU bound | Backpressure surfaces |
| BP-029 | Concurrent force_flush calls | 100 parallel | All return same result |
| BP-030 | Span processor re-entrancy | Processor emits a span | No infinite loop (loop guarded) |

## Protocol Conformance OTLP (PROTO)

| ID | Title | Intent | Expected |
|---|---|---|---|
| PROTO-001 | gRPC unary export traces | Default transport | 200 OK, decoded by collector |
| PROTO-002 | gRPC unary export metrics | Metrics path | OK |
| PROTO-003 | gRPC unary export logs | Logs path | OK |
| PROTO-004 | HTTP/protobuf traces | `otlp_http_protobuf` | OK |
| PROTO-005 | HTTP/protobuf metrics | Metrics | OK |
| PROTO-006 | HTTP/protobuf logs | Logs | OK |
| PROTO-007 | HTTP/json traces | If supported | OK or graceful unsupported |
| PROTO-008 | gzip content-encoding | Compress payload | Collector decodes |
| PROTO-009 | identity content-encoding | No compression | OK |
| PROTO-010 | Chunked transfer | HTTP/1.1 chunked | Accepted |
| PROTO-011 | HTTP/2 multiplexing | Many concurrent reqs | All succeed |
| PROTO-012 | HTTP/2 GOAWAY handling | Server GOAWAY | Reconnect |
| PROTO-013 | gRPC UNAVAILABLE | 503-equivalent | Retry with backoff |
| PROTO-014 | gRPC RESOURCE_EXHAUSTED | Throttle | Retry honoring Retry-After |
| PROTO-015 | gRPC DEADLINE_EXCEEDED | Timeout | Retry next interval |
| PROTO-016 | gRPC INVALID_ARGUMENT | Bad payload | Drop, no retry, log |
| PROTO-017 | gRPC INTERNAL | Server bug | Retry bounded |
| PROTO-018 | gRPC UNAUTHENTICATED | Auth fail | No retry, bubble up |
| PROTO-019 | gRPC PERMISSION_DENIED | Authz fail | No retry |
| PROTO-020 | HTTP 200 partial success | Partial response | Count rejected items |
| PROTO-021 | HTTP 400 | Bad request | Drop |
| PROTO-022 | HTTP 401/403 | Auth | No retry |
| PROTO-023 | HTTP 404 | Wrong path | Drop, log clear |
| PROTO-024 | HTTP 408 | Request timeout | Retry |
| PROTO-025 | HTTP 429 with Retry-After | Throttle | Backoff per header |
| PROTO-026 | HTTP 500 | Server error | Retry |
| PROTO-027 | HTTP 502/503/504 | Gateway errors | Retry |
| PROTO-028 | Malformed proto bytes | Fuzz test outbound | Never produced |
| PROTO-029 | Unknown collector field | Forward compat in response | Ignored |
| PROTO-030 | Oversized message >4MB | Hits gRPC limit | Auto-split or drop with metric |
| PROTO-031 | Oversized HTTP body | Server max body | Server 413; SDK drops |
| PROTO-032 | schema_url propagation | Resource schema_url set | Encoded in OTLP |
| PROTO-033 | InstrumentationScope name/version | Set on tracer | Encoded |
| PROTO-034 | Span status codes mapping | OK/ERROR/UNSET | Encoded correctly |
| PROTO-035 | Span kinds mapping | Internal/Server/Client/Producer/Consumer | Encoded correctly |
| PROTO-036 | Trace flags sampled bit | Sampled span | Bit set |
| PROTO-037 | Trace state preserved | Multi-vendor tracestate | Round-trip preserved |
| PROTO-038 | Resource attrs encoded once | Common resource | Once per ResourceSpans |
| PROTO-039 | Empty batch handling | No spans to export | Skipped, no request |
| PROTO-040 | Endpoint trailing slash | `http://x/` vs `http://x` | Both work |

## Security & Auth (SEC)

| ID | Title | Intent | Expected |
|---|---|---|---|
| SEC-001 | TLS 1.2 connect | Force TLS 1.2 | Connects |
| SEC-002 | TLS 1.3 connect | Force TLS 1.3 | Connects |
| SEC-003 | TLS downgrade rejected | Server offers SSLv3 | Refuses |
| SEC-004 | mTLS happy path | Client cert presented | Accepted |
| SEC-005 | mTLS missing client cert | None presented | Server 401 / TLS error |
| SEC-006 | Cert rotation hot reload | Rotate file under sni | Picks up new cert |
| SEC-007 | Expired server cert | Past `notAfter` | Connection refused |
| SEC-008 | Hostname verification | Wrong CN/SAN | Refused |
| SEC-009 | Hostname verification disabled | Insecure mode | Connects with warning |
| SEC-010 | SNI sent | Verify ClientHello SNI | Matches host |
| SEC-011 | Custom CA bundle | Private CA | Accepted |
| SEC-012 | System CA bundle | Default trust | Accepted |
| SEC-013 | Cipher suite restriction | Strict list | Negotiates within list |
| SEC-014 | OCSP stapling | Server staples | Accepted |
| SEC-015 | CRL check (if implemented) | Revoked cert | Refused |
| SEC-016 | Header redaction | `Authorization` redacted in logs | Not present |
| SEC-017 | Bearer token static | Header set | Sent on each req |
| SEC-018 | Bearer token refresh | Token provider callback | New token used |
| SEC-019 | OAuth2 client_credentials | If supported | Token fetched, refreshed before expiry |
| SEC-020 | Header injection prevention | CRLF in header | Rejected by client |
| SEC-021 | PII in attribute key | `password` literal key | Documented; user responsibility |
| SEC-022 | Attribute value length limit | Limit truncates | Truncated cleanly |
| SEC-023 | Attribute count limit | Excess dropped | Dropped count recorded |
| SEC-024 | Event count limit | Excess dropped | Dropped count recorded |
| SEC-025 | Link count limit | Excess dropped | Dropped count recorded |
| SEC-026 | Env var leakage | `OTEL_*` not echoed | Logger does not print secrets |
| SEC-027 | Proxy basic auth | `http_proxy` with creds | Auth header set, redacted in logs |
| SEC-028 | HTTPS_PROXY honored | TLS via proxy | Connects via CONNECT |
| SEC-029 | NO_PROXY honored | Bypass list | Direct connect |
| SEC-030 | Headers env var parsing | `OTEL_EXPORTER_OTLP_HEADERS=a=b,c=d` | Parsed correctly, escapes handled |

## Context Propagation (CTX)

| ID | Title | Intent | Expected |
|---|---|---|---|
| CTX-001 | W3C traceparent valid | Standard header | Extracted, span linked |
| CTX-002 | traceparent invalid version | `ff-...` future | Treated as no parent (per spec) |
| CTX-003 | traceparent malformed | Bad hex | Ignored, no parent |
| CTX-004 | traceparent all-zero trace_id | Invalid | Ignored |
| CTX-005 | traceparent all-zero span_id | Invalid | Ignored |
| CTX-006 | tracestate single vendor | `vendor=value` | Preserved |
| CTX-007 | tracestate multi vendor | Up to 32 entries | Preserved, order maintained |
| CTX-008 | tracestate exceeds 512 chars | Trim per spec | Trimmed correctly |
| CTX-009 | baggage simple | `k=v` | Extracted |
| CTX-010 | baggage URL-encoded | `k=%20v` | Decoded |
| CTX-011 | baggage size limits | >8192 bytes | Truncated/dropped per impl |
| CTX-012 | baggage charset | Forbidden chars | Rejected/escaped |
| CTX-013 | baggage TTL/metadata | `;ttl=60` | Preserved |
| CTX-014 | gen_server call propagation | Manual ctx pass | Span continues |
| CTX-015 | gen_server cast propagation | Manual ctx pass | Span continues |
| CTX-016 | gen_statem propagation | Manual ctx pass | Works |
| CTX-017 | proc_lib spawn propagation | Spawn helper | Ctx in child |
| CTX-018 | Cross-node Erlang dist | RPC call | tracecontext propagated explicitly |
| CTX-019 | RabbitMQ headers | Inject AMQP headers | Round-trips |
| CTX-020 | Kafka headers | Brod producer | Round-trips |
| CTX-021 | NATS headers | Headers carrier | Round-trips |
| CTX-022 | hackney inject | HTTP client | traceparent on outbound |
| CTX-023 | gun inject | HTTP/2 client | traceparent on outbound |
| CTX-024 | httpc inject | OTP client | traceparent on outbound |
| CTX-025 | cowboy extract | HTTP server | parent set on req span |
| CTX-026 | elli extract | HTTP server | parent set |
| CTX-027 | Mochiweb extract | HTTP server | parent set |
| CTX-028 | Phoenix endpoint extract (Elixir) | Plug | parent set |
| CTX-029 | Span links from carrier | Multiple parents via links | Links populated |
| CTX-030 | Remote parent sampled honored | Parent sampled=1 | Child sampled |
| CTX-031 | Remote parent not sampled | Parent sampled=0 with parent_based(always_off) | Child not sampled |
| CTX-032 | Composite propagator order | tracecontext+baggage | Both injected/extracted |
| CTX-033 | Custom propagator | User propagator module | Hooked correctly |
| CTX-034 | otel_ctx detach safety | detach in wrong order | No crash, returns error |
| CTX-035 | otel_ctx with_span | Closure cleanup on raise | Ctx restored |

## Sampling & Resource Detection (SAMP)

| ID | Title | Intent | Expected |
|---|---|---|---|
| SAMP-001 | always_on | Sampler | All sampled |
| SAMP-002 | always_off | Sampler | None sampled |
| SAMP-003 | trace_id_ratio 0.0 | Edge | None sampled |
| SAMP-004 | trace_id_ratio 1.0 | Edge | All sampled |
| SAMP-005 | trace_id_ratio 0.5 | Mid | ~50% within tolerance |
| SAMP-006 | trace_id_ratio 1e-9 | Tiny | Statistically zero, no div-by-zero |
| SAMP-007 | trace_id_ratio negative | Invalid | Rejected at config |
| SAMP-008 | trace_id_ratio >1 | Invalid | Rejected at config |
| SAMP-009 | parent_based + always_on | Root sampler | Root always; child follows parent |
| SAMP-010 | parent_based + always_off | Root never | Children follow parent |
| SAMP-011 | parent_based + ratio | Root ratio | Children follow parent |
| SAMP-012 | parent_based remote_parent_sampled | Override | Honored |
| SAMP-013 | parent_based remote_parent_not_sampled | Override | Honored |
| SAMP-014 | parent_based local_parent_sampled | Override | Honored |
| SAMP-015 | parent_based local_parent_not_sampled | Override | Honored |
| SAMP-016 | Custom sampler returning RECORD_ONLY | Records, not exported | Span recorded but not in exporter output |
| SAMP-017 | Custom sampler returning RECORD_AND_SAMPLE | Standard sample | Exported |
| SAMP-018 | Custom sampler returning DROP | Drop | Not recorded |
| SAMP-019 | Sampler attributes mutation | Sampler adds attrs | Present on span |
| SAMP-020 | Sampler tracestate update | Sampler updates ts | Propagated |
| SAMP-021 | Sampler crash isolation | Raises | Span dropped, tracer survives |
| SAMP-022 | Rate limit sampler | N/sec cap | Cap enforced |
| SAMP-023 | Attribute-based sampler | `http.url=/health` drop | Health checks dropped |
| SAMP-024 | Composite sampler | Chain of samplers | First decisive wins |
| SAMP-025 | Resource detector env | `OTEL_RESOURCE_ATTRIBUTES=k=v` | Parsed |
| SAMP-026 | Resource detector env escaped | `k=a%2Cb` | Decoded |
| SAMP-027 | Resource detector host | host.name set | Matches `inet:gethostname` |
| SAMP-028 | Resource detector os | os.type/os.version | Populated |
| SAMP-029 | Resource detector process | process.pid, runtime.* | Populated, runtime=BEAM |
| SAMP-030 | Resource detector container | cgroup parse | container.id when in container |
| SAMP-031 | Resource detector k8s | downward API env | k8s.* populated |
| SAMP-032 | Resource merge precedence | env > detector > default | Honored |
| SAMP-033 | service.name fallback | Unset | Defaults to `unknown_service:<beam>` |
| SAMP-034 | OTEL_SERVICE_NAME wins | Set env | Wins over resource attrs |
| SAMP-035 | Resource schema_url chosen | Multiple detectors | Newest schema_url chosen per spec |

---

## Summary

| Section | Prefix | Count |
|---|---|---|
| Lifecycle & Supervision | LIFE | 30 |
| Failure Injection / Chaos | CHAOS | 45 |
| Load & Throughput | LOAD | 40 |
| Concurrency & Scheduler | CONC | 35 |
| Backpressure & Buffering | BP | 30 |
| Protocol Conformance OTLP | PROTO | 40 |
| Security & Auth | SEC | 30 |
| Context Propagation | CTX | 35 |
| Sampling & Resource Detection | SAMP | 35 |
| **Total** | — | **320** |
