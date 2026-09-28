# Source fanout measurement

Run:

```sh
go test ./service/cdc/postgres -run '^$' -bench BenchmarkSourceFanout -benchmem -benchtime=100x -count=3
```

The benchmark waits for every subscriber to receive each event. It measures
source filtering, bounded queue handoff and subscriber scheduling, not PostgreSQL,
network transport, the Wippy relay or Lua conversion. It does not establish an
end-to-end throughput or latency guarantee.

On the development Ryzen 9 7950X3D host, 1000 subscribers took roughly 0.63–0.71 ms
per broadcast before queue reuse and 0.54–0.59 ms afterward. Allocation fell from
about 297 KB / 1006–1012 allocations to 11–12 KB / 14–18 allocations per broadcast.
These short runs are comparative observations, not capacity planning limits.

The change retains empty queue capacity and clears consumed payload references.
It does not introduce a ring-buffer framework, extra goroutines or new queues.
Source scanning remains linear in subscriber count, matching broadcast's
unavoidable delivery work. Do not add an interest index or shared-worker layer
without profiling representative filtered workloads.
