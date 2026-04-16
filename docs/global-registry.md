# Global Registry: How Name Uniqueness Works

## The Problem

```lua
process.registry.register("payments", process.registry.GLOBAL)
```

You want exactly one process named `"payments"` across the entire cluster. Not "eventually one". Not "probably one". **Exactly one, at all times.** This is harder than it sounds.

## Why It's Hard

In a distributed cluster, two nodes can try to register the same name at the same time. Without coordination, both succeed, and now you have two processes claiming the same name. Classic split-brain.

You need consensus. That's where Raft comes in.

## How Raft Solves It

The cluster runs a [Raft consensus group](https://raft.github.io/). One node is the **leader**, the rest are followers. All writes go through the leader.

When you call `register("payments", GLOBAL)`:

1. The request reaches the local node.
2. If this node isn't the Raft leader, it **forwards** the request to whoever is.
3. The leader proposes a `CmdRegister` entry to the Raft log.
4. Raft replicates the entry to a **majority** of nodes before committing.
5. Every node applies the command to its local state machine (FSM) **in the same order**.

The FSM apply logic is simple:

- Name already taken by a **different** PID? Return `ErrNameAlreadyRegistered`. Registration fails.
- Name already taken by the **same** PID? Idempotent. Return success.
- Name free? Insert it. Return success.

Because Raft serializes all writes through a single leader and applies them in log order, two concurrent `register("payments")` calls are **linearized**. One wins, one loses. Always.

This gives you a **linearizable** guarantee: after a successful register, every node in the cluster will agree that name belongs to your process.

## The Sharding Layer

Names are distributed across **16 shards** using FNV32a hashing. Each shard is an independent chunk of the state. This matters for concurrent throughput -- operations on different shards don't contend with each other.

For a single name registration, this is transparent. The name hashes to one shard, the command applies to that shard, done.

## Why Multi-Name Registration Needs 2PC

Sometimes you need to register multiple names atomically (via `RegisterMulti`). If all names hash to the same shard, it's straightforward -- one Raft command handles it.

But if the names land on **different** shards, you have a problem: you need atomicity across independent state partitions. If `"payments"` goes to shard 3 and `"billing"` goes to shard 7, you can't just apply them independently -- if one fails, you need to roll back the other.

This is a textbook distributed transaction problem. The solution is **two-phase commit (2PC)** on top of Raft:

### Phase 1: Prepare

Each involved shard checks if the operation can succeed:
- For register: are all the names in this shard available?
- For unregister: do all the names in this shard exist?

All shards prepare concurrently. If **any** shard fails to prepare, the whole transaction aborts.

### Phase 2: Commit

All shards apply the commands concurrently. Each shard's apply goes through the same Raft FSM path, so it inherits the same linearizability guarantees.

### Optimization

Single-name operations skip 2PC entirely. Same-shard multi-name operations also skip it. 2PC only kicks in when names actually span multiple shards. In practice, most registrations are single-name, so 2PC is the exception.

## Cleanup

### Process Exits

When a globally registered process dies, the runtime automatically unregisters all its names via Raft. Other processes doing `lookup("payments")` will get `nil` after the unregister command is committed.

### Node Leaves

When a node leaves the cluster (gracefully or via failure detection), **all** names registered by processes on that node are removed. The `CmdRemoveNode` command sweeps the node index and cleans everything up in one shot.

## Local vs Global Interaction

The local registry (`process.registry.LOCAL`, the default) checks the global registry **before** allowing a local registration. If `"payments"` is already registered globally, you can't register it locally either. This prevents accidental shadowing.

Lookups check global first, then local. Global names always win.

## Summary

| Guarantee | Mechanism |
|---|---|
| Cluster-wide uniqueness | Raft consensus (single leader, linearizable log) |
| Atomicity across shards | 2PC coordinator on top of Raft FSM |
| Automatic cleanup on crash | Process exit / node leave handlers via Raft |
| No local shadowing | Local registry checks global before registering |

The core insight: Raft gives you a linearizable log. That log serializes all name operations. If two nodes race to register the same name, the log decides who wins. Everything else -- sharding, 2PC, cleanup -- is built on top of that foundation.
