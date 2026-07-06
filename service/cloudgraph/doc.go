// SPDX-License-Identifier: MPL-2.0

// Package cloudgraph is a proof-of-concept vertical slice of the
// wippy.cloud.graph deployment control plane designed in
// docs/design/cloud-graph.md. It proves the risky seams end to end:
// deterministic plan computation (gonum graph, Kahn waves), a deterministic
// Go Temporal deploy workflow, per-operation execute_operation activities,
// Lua provider actors spawned over the process host, and the durable
// operation_signals mailbox with CAS-fenced acceptance.
//
// PoC relaxations against the design (each section cited is the design
// document's normative treatment; none of these relaxations weaken what the
// PoC claims to prove):
//
//   - Locks and fencing tokens (design §8 Claim, I1/I8): only the actor
//     incarnation CAS and the per-incarnation signal seq CAS are enforced.
//   - Generation CAS on signal acceptance (C2): pinned to 0; the
//     operation_signals schema keeps the fencing_token and
//     resource_generation columns so the fences are an additive change.
//   - Deploy classification (design §8.1): create-only; no canonical
//     baseline, no update/delete/replace, no targeted deploys.
//   - operation_blockers (design §8.4): replaced by dispatch-time gate
//     re-checks reading committed producer outputs.
//   - Observer/watchers, compensation, and the three-phase cancel protocol
//     (design §9.5, §9.6, §11): not implemented; cancellation delivers the
//     cancel package to the actor and terminalizes.
//   - Recovery (design §9.7): limited to the idempotent already-terminal
//     check, incarnation fencing, orphan-cancel, and checkpoint replay on
//     the next activity attempt.
//   - Ledger dialects (design §10.3): SQLite only.
//   - Provider contracts (design §7.3): apply actors only; validate, diff,
//     refresh, import, and create_watcher are not implemented.
//   - One engine per installation; the shard field is informational.
//   - Engine entry deletion mid-deploy is unsupported: published handlers and
//     registered workflow/activities are not unregistered, so the entry must
//     only be removed at shutdown.
package cloudgraph
