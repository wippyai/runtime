# Asyncify transformation and continuation contracts

Status: proposed architecture for the W1 backend, with portable W2 semantics.
No new pipeline implementation or production-readiness claim is implied by this document.
Keep work local; do not push without renewed user authorization.

## Problem established by evidence

The current backend has an opcode handler registry, but stack simulation,
control-flow lowering, temporary allocation, and byte emission still duplicate
instruction behavior. The old local.tee handler retained a mutable local index
instead of a value snapshot: a regression returned 198 where original Wasm
returned 106, both with and without suspension. A handler interface alone did
not prevent semantic disagreement.

The corrected port's PDF main core has 64,056 locals. Optimized upstream
Binaryen 132 has 23,983. Disabling its pre-flow optimization group raises that
to 69,553 despite keeping later cleanup. These are structural measurements;
they do not assign execution-time benefit to one pass. The matched full-upstream
experiment was 15.4% faster for one synthetic PDF workload, with identical output.
This does not establish acceptance for other workloads or sustained actor load.

## Two contracts, kept separate

1. The runtime continuation protocol defines normal, unwinding and rewinding
   transitions, ownership, suspension completion, cancellation and cleanup.
   It is independent of Lua shapes, W1/W2 scheduling, and compiler strategy.
2. The compiler transformation contract defines typed representations, legal
   rewrites, analysis dependencies, frame planning and emission. It implements
   the continuation protocol while preserving supported Wasm semantics.

Actor messaging, sockets, and host services use the first contract. They must
not depend on generated local indices, branch depths, or transform internals.
Asyncify provides cooperative suspension. Cancellation that destroys execution
is not resumable preemption; future fuel scheduling needs its own explicit contract.

## Runtime protocol

Maintain the existing five-function Asyncify control ABI and three states.
One active invocation owns each instance's continuation. Do not allow two host
operations to rewind the same instance concurrently. An unwound invocation
releases its executing worker while retaining its continuation ownership.

A host operation distinguishes first invocation from replay. On first invocation
it records the pending operation and requests unwind. After the outer guest call
returns unwound, the runtime stops unwind. Completion makes the continuation
runnable. Resumption starts rewind and re-enters the same root invocation;
the suspension boundary stops rewind and consumes the recorded result.
Replay must not repeat the externally observable operation. This is local
invocation behavior, not an exactly-once distributed message delivery guarantee.

Define and test allowed transitions, including repeated suspension, errors,
traps, cancellation and close races. Teardown releases pending host resources,
continuation memory and instance references once. Reject invalid state changes
at the owning runtime boundary rather than relying on optimizer assumptions.

A component may span several core modules and adapters. Resolve actual bound
import identities and suspension effects before transforming their callers.
Adapter/table edges and interface versions are part of this resolution, not
hard-coded string exceptions inside the code generator. Unknown indirect
reachability remains conservative unless proved otherwise.

## Typed compiler representations

Use stable FunctionID, InstructionID, ValueID and LabelID identities. Resolve
binary indices and relative branch depths at decoding/emission boundaries.
Do not use a guest-local index as the identity of an operand value.

An opcode descriptor supplies feature requirements, context-dependent input
and output types, control effects, local/global/memory/table effects, trapping
behavior and call targets where applicable. There is one authoritative semantic
resolver used by lowering, verification and analyses. Missing semantics reject
transformation of affected code; they must never imply a pure/no-op instruction.
Freeze registries before concurrent compilation. Extensions register complete
semantics and tests; they cannot silently replace a standard opcode's meaning.

Lower stack operands to typed values and model guest locals as mutable cells.
For local.tee, assign the cell and retain the same immutable operand value.
Structured branches carry typed values to explicit labels/block parameters.
Handle unreachable stack polymorphism explicitly, not with an invented i32
fallback. A supported-feature profile must state limitations such as reference
values across suspension, memory64, exceptions and tail calls.

## Stages and their obligations

| Stage | Output | Required invariant |
|---|---|---|
| Decode and validate | Typed module and feature profile | Valid types, indices, labels and supported instructions |
| Resolve suspension effects | Module/call-site effect graph | Every reachable suspension path is covered, including adapters and indirect edges |
| Lower values and structured control | Typed value IR with stable identities | Original evaluation order, snapshots, branch values and traps preserved |
| Simplify values and plan local reuse | Verified pre-flow IR | No overlapping live values share storage; replay-required values remain represented |
| Insert continuation flow | Explicit suspend/replay control IR | Ordinary effects execute once; rewind reaches the recorded call site |
| Plan frames | Immutable continuation/frame plan | Every value needed after suspension or during replay is recoverable with correct type |
| Emit and clean up | Valid Wasm plus provenance | Code consumes the exact plan; cleanup preserves flow and frame semantics |

This is a fixed internal pipeline with explicit stages, not a general-purpose
plugin system. Optimization passes declare which analyses they require and
invalidate. Analyses are tied to an IR revision; stale call-site, liveness or
allocation maps cannot be consumed after a rewrite. Verify types and control
flow between representation-changing stages. Keep more expensive checks in
verification/CI modes where appropriate; essential bounds and protocol checks
remain production requirements.

Build one immutable continuation plan containing call-site identity, replay
routing, typed saved values, frame layout, memory selection and bounds. Save
and restore emission consume the same plan. Do not independently recalculate
layout or rely on duplicate allocation simulations agreeing by convention.
Use checked arithmetic for frame sizes and pointers. Numeric local indices are
an emission detail, not the continuation protocol.

Normal-flow liveness alone is insufficient: a value can be needed to route
replay even when the original continuation no longer reads it. Preserve these
requirements explicitly and verify them again after flow insertion. Any guard
hoisting must account for all permitted state-changing calls and effects.

Record transform version, feature profile, resolved suspension effects and
optimization configuration in compilation-cache identity. Persisted generated
artifacts must not be reused with incompatible runtime/frame assumptions.
There is no requirement to resume an in-memory continuation across runtime versions.

## Local implementation progress

A first frame-plan prototype is implemented in an isolated backend checkout.
It constructs owned typed slots once, validates indices/types/size, and supplies
the same offsets and frame size to save and restore emission. It preserves the
captured PDF core byte-for-byte. Asyncify and backend-engine race suites,
32-bit plan checks, and lint pass. This prototype is not adopted or published.
Typed value IR, shared opcode semantics, and analysis invalidation are still
required; the frame-plan extraction alone does not complete this architecture.

The next local prototype shares a checked description of local.get/local.set/
local.tee between simulation and standard-handler emission. It also resolves
original local identities before generating new locals: a regression proved
that a previously invalid guest index could otherwise alias generated scratch.
The updated race suites and malformed-input checks pass, with byte-identical
captured PDF output. Full typed operands/control flow and extension-registry
contracts remain pending.

A third local slice shares checked global identity/type/mutability resolution
between simulation and emission. Invalid metadata no longer produces an invented
i32 type. Race tests and lint pass; restoring the old code fails the new rejection
tests. The captured PDF output remains byte-identical. Current-module
resolution alone cannot establish original index validity; the next slice below
adds the original-module boundary.

The latest local slice runs input identity preflight immediately after parsing,
before any insertion or reindexing. It reuses structural section validation and
shared global semantics, checks direct function references, and visits code and
constant-expression storage through one read-only traversal. Seven regressions
prove that the old transformer turned invalid guest identities into valid
generated references; the new boundary rejects them. Full backend race tests
(with loopback permission) and lint pass; captured PDF transformation is unchanged.

This also replaces a test helper that fabricated modules from WAT substrings
with the actual WAT compiler. Fixture compilation now fails tests on error.
Instruction type/table/memory/GC validation and existing section-validator gaps
are not solved by this preflight. The extra decoding pass is not an optimization;
future typed input should be decoded once and reused by stages.

Call semantics are now shared in a further local prototype. A frozen module
snapshot owns signatures/function bindings; successful resolution allocates no
memory per call. Call-site planning, simulation and emission consume checked
call descriptions, including the extra dynamic target operand. Original-input
preflight uses the same resolver and rejects an invalid call type that would
otherwise alias a generated helper type. Type snapshots use one flat traversal,
including recursive type groups. Full backend race tests and lint pass.
This is not a complete typed value IR or enforced analysis-revision protocol.

A separate local optimization removes seven unused scratch declarations per
transformed function. On the captured PDF main core this reduces declared locals
from 64,056 to 46,794 across 2,466 functions, versus 23,983 in optimized upstream.
The remaining excess-local problem is open. A per-function structural check
proves that the only changes are removing unreferenced declarations and
renumbering subsequent local accesses. Independent Wasm validation, Asyncify/
backend-engine/wasm race suites and lint pass. Tests now verify continuation
state/data/frame access instead of demanding the obsolete scratch count.

The semantics-only PDF artifact remains 8,890,692 bytes; the scratch candidate
is 8,856,168 bytes and is NOT byte-identical. These scoped core comparisons do
not establish byte equality with Binaryen, guest execution speed, or application
performance. No new PDF warm benchmark has been run for these local slices.

## Required contract for each migrated instruction family

An emitter registration alone is insufficient. Each migration must provide:

- A checked semantic resolver with explicit distinction between unrelated and
  malformed instructions; missing semantics cannot fall through as a no-op.
- Context bound to the representation being resolved. Validate original indices
  before generated locals, globals or functions extend their index spaces.
- Owned typed descriptions consumed by both analysis and emission. Temporary
  legacy simulations must fail on disagreement instead of substituting types.
- Explicit value, cell, control and suspension effects as applicable. A cell read
  produces a snapshot; it is not an alias that a later write may change.
- Evidence for normal and suspended behavior, side effects and traps; invalid
  context tests must check that failure precedes stack/emitter mutation.
- Declared analysis dependencies and invalidation rules for later rewrites.

This is the migration acceptance contract, not a claim that the current registry
already enforces it. Migrate calls and branch-value routing next, then replace
positional simulation with typed values and enforce revision validity. Avoid an
unused generic pass framework while the actual consumers remain on legacy paths.

## Migration with reviewable evidence

1. Preserve and isolate current correctness fixes and regression oracles.
2. Introduce typed values and the shared semantic resolver for local operations
   first. Verify lowering independently against original Wasm semantics.
3. Move call-site and frame planning into explicit typed plans without changing
   the external ABI. Compare emitted behavior and frame traces across repeated
   suspension, branching, recursion and host failures.
4. Eliminate the second implementation of stack/allocation behavior as handlers
   migrate. A temporary legacy adapter must report disagreement, not conceal it.
5. Add pre-flow simplification/reuse only after the typed representation and
   verifier exist. Measure before/after using identical guests and suspension roots.
6. Run the actor, SQLite, CPython, hub R, PDF/render, memory and load gates before
   claiming the architecture is ready. W2 adopts the same contract separately.

For every new opcode or pass: valid raw execution must agree with transformed
normal and suspended execution; side effects, traps, typed branch values and
memory outcomes are explicit oracles. Compare optimized and unoptimized
upstream where supported, on both Wazero compiler and interpreter. Upstream
agreement supplements the Wasm specification; it does not replace it.

## References and current evidence

- Wasm local.tee execution: https://webassembly.github.io/spec/core/exec/instructions.html#exec-local-tee
- Binaryen 132 Asyncify contract and pipeline: https://github.com/WebAssembly/binaryen/blob/version_132/src/passes/Asyncify.cpp
- Binaryen optimization defaults: https://github.com/WebAssembly/binaryen/blob/version_132/src/tools/optimization-options.h
- Existing release gates: wasm_validation_plan.md and wasm_workloads.md in this directory.
- Local reproducible stage evidence: /tmp/w1-binaryen-stage-results/findings.md
- Local matched full-upstream experiment: /tmp/w1-binaryen-internal-opt/comparison.md

Binaryen's level 2 also enables StackIR emission cleanup. Stage controls proved
that toggling it leaves the fully optimized PDF artifact identical, but changes
the all-skipped artifact. Preserve that distinction in future experiments.

## Ownership prototype and priority clarification (2026-09-06)

Execution performance is the primary performance objective; transformation speed
is secondary. The user authorizes replacement of the architecture. Correctness,
explicit phase ownership and maintainability remain acceptance requirements.

Local prototype `/tmp/w1-value-reuse-prototype` now separates cell knowledge,
typed temporary storage/definition identities, materialization planning and an
independent emission verifier. See its `VALUE_LAYERS_REVIEW.md` for invariants,
fault injection and the exact remaining phase boundaries. Eight guest fixtures
compare original, Go and upstream Binaryen on both Wazero backends, including
repeated suspension and host-effect counts. This is a verified local slice, not
a completed implementation of the typed pipeline proposed above.

The larger change remains moving typed value/lifetime analysis before synthetic
routing expands control flow, with downstream analyses explicitly invalidated
by rewrites. Upstream Binaryen 132 documents this ordering in Asyncify.cpp
lines1856–1918. Do not equate a cache extraction or fewer locals with that change.

The matched scratch-removal PDF diagnostic found no useful warm speedup:
778.2965ms control versus778.856ms candidate, two AB/BA pairs, all80 outputs
matched. No execution-performance claim is established for the value prototype.

## Checked control input boundary (2026-09-06)

A subsequent local slice replaces permissive tree parsing with a checked frame
parser and shared block-signature resolution against the correct flattened type
space. It rejects malformed function control and original invalid type indices
before helper types can make them valid. Four negative fixtures reproduce the
old transformation accepting invalid Wasm and producing valid output. Full
backend race tests and lint pass. See
`/tmp/w1-control-boundary-prototype/CONTROL_BOUNDARY_REVIEW.md` for exact evidence.

This establishes structural input for the proposed typed pipeline. Operand type
validation, stable LabelIDs, analysis invalidation and moving storage planning
before synthetic routing remain unfinished. The parser slice preserves bytes
for the captured PDF core relative to the preceding value-layer prototype; it
makes no execution-performance claim. The value prototype reduces declared
locals by only131 relative to scratch removal, so general pre-flow coalescing
remains necessary to address the measured upstream gap.

## Owned source analysis and bound targets (2026-09-06)

The next local slice establishes a real source-control/suspension boundary:
Prepare owns its decoded tree, copied signatures, source label/call identities
and suspension facts. Lowering consumes this private plan and transfers owned
instructions plus checked suspension sites. Branches derive depths from bound
labels, and the engine no longer reclassifies suspension after routing. See
`/tmp/w1-source-analysis-prototype/SOURCE_ANALYSIS_REVIEW.md`.

Representing the implicit function target exposed a reproduced wrong-result bug:
a br_table function exit returned its selector1/9 instead of value7. A referenced
function target now becomes an ordinary outer result frame, using the same spill
contract as block exits. Differential tests cover scalar/multiple results,
conditional exits and loop-parameter/function-target combinations with suspension.
Agy supplied the review lead; root reproduced the old behavior and verified the fix.

Final targeted race/lint pass, and the frozen PDF smoke produces the expected
markdown hash. No execution speedup is claimed. Typed operand/value liveness and
storage planning still run after routing; moving those analyses onto the owned
source representation remains the next architectural dependency.

## Source operand plan (2026-09-06)

The cumulative local `/tmp/w1-typed-values-prototype` adds mandatory
`Prepare -> PlanValues -> Linearize`. Operand identity, control parameter/result
ports and pre-call continuation operands now exist before generated routing.
Graph ownership, stack rules, control traversal and instruction effects live in
separate units. Scalar signatures are shared across planning and emission, with
all128 base operations independently validated using Wazero.

New negative comparisons show the preceding transformer accepting five invalid
source operand programs; three even produced independently valid output. Source
planning rejects them. Additional dead-code tee/br_if regressions ensure declared
output types are preserved when consuming polymorphic unknown operands. Historical
invalid stress/benchmark fixtures are repaired and independently validated.

See `/tmp/w1-typed-values-prototype/TYPED_VALUES_REVIEW.md` for exact evidence and
limits. Full backend race, final scoped race and lint pass. The frozen 96-page
PDF smoke retains the expected markdown hash; this establishes no speedup.

This source graph does not yet drive physical allocation. Complete CFG program
points, effect contracts, reference subtyping and source lifetime allocation
remain unfinished. Opaque input signatures/control transfers must not be treated
as complete optimization semantics. Downstream emitted-instruction simulation
still exists and must be replaced through a checked allocation/lowering contract.

## Control storage and rewind execution domains (2026-09-06)

`/tmp/w1-source-storage-prototype` moves control-carrier requests before emission,
assigns typed slots using explicit scope events, verifies live-owner/type contracts,
and binds physical locals before routing starts. Emission no longer has an
allocator. Disjoint completed scopes can reuse carriers; ancestors stay distinct,
and opaque control disables reuse. General source operand allocation is still open.

A new nested/sequential-if differential exposed an earlier rewind bug: return1
instead of16, reproduced with the preceding implementation and with reuse disabled.
Routing calculations overwrote restored guest operands because their normal-path
temporary lifetimes appeared disjoint. Temporary pools now distinguish guest and
routing execution domains as well as type. The reproducer matches original Wasm
and Binaryen132 on both Wazero backends, normal and resumed.

See `/tmp/w1-source-storage-prototype/SOURCE_STORAGE_REVIEW.md` for origin overlays,
assignment-verifier tests and final race/lint evidence. The frozen PDF smoke keeps
its expected hash; its captured core is unchanged, so no PDF speedup is established.

## Continuation contracts (2026-09-06)

`/tmp/w1-continuation-contract-prototype` carries immutable source operand IDs/types
at each lowered suspension. Later stack simulation must match source count/types
and must not save guest operands in routing-temporary storage. Shared materialization
domains are independently checked during emission, including cross-domain aliases.
Generated if-arm parameter reads are guest materializations restored by continuation;
only routing predicates are recomputed during rewind.

Full backend race, lint and PDF output verification pass. See the prototype's
CONTINUATION_CONTRACT_REVIEW.md. This is not yet exact source-definition identity
proof or general source allocation. Saving only active control carriers is a
possible next optimization, currently unimplemented and requiring a rewind proof.

## Source-active saved carriers (2026-09-06)

`/tmp/w1-active-carriers-prototype` binds each suspension to its enclosing source
control carriers. The frame builder saves their union alongside ordinary live
locals and source-checked operand snapshots. Completed scopes contribute no new
carriers; opaque control retains the conservative all-carrier set.

Rewind regressions cover completed branches/loops/tables and active ancestors,
including exact host-effect counts. An actual unwind measurement falls from20 to8
bytes for the same fixture on both Wazero backends; resumed result/state/count stay
correct. This is a save/restore traffic reduction, not a workload timing claim.
Full/scoped race, lint and PDF output verification pass. See ACTIVE_CARRIERS_REVIEW.md
for the routing obligations and old-engine overlay. The captured PDF core remains
unchanged; general source operand allocation is still required for its local gap.

## Call-result lifetimes and matched PDF check (2026-09-06)

`/tmp/w1-call-result-lifetimes-prototype` removes permanent per-call result locals.
Results become checked guest operand definitions, with call inputs held until
instruction completion. The provisional-unwind-result regression proves arguments
survive rewind. A32-call fixture falls from36 to4 locals and132 to4 frame bytes.
Full backend race/lint pass.

This reaches the PDF core:46663 ->44093 locals and51285 fewer bytes. Matched remote
AB/BA,40 warm calls per variant, instead finds777.300ms control vs783.247ms candidate
(+0.765%,slower), with all outputs matching. Cold compile improves5.159% in that
sample. There is no PDF warm speedup or adoption; fewer locals are not acceptance.
See CALL_RESULT_LIFETIMES_REVIEW.md and `/tmp/w1-call-results-pdf-pairs/comparison.md`.

General source provenance/immediate-vs-storage lowering and full lifetime allocation
remain next. A large remaining case materializes many constant/arithmetic arguments
into locals, reinforcing the need to replace the local-only operand representation.

## Exact literal provenance (2026-09-06)

`/tmp/w1-literal-values-prototype` shares numeric/vector Literal semantics across
source planning and emission. Source continuations retain exact bits independently
of storage, including tee identity forwarding. Mutable reads/arithmetic are not
inferred constant. Independent execution checks NaN payloads, signed zero and
vector bits on both Wazero backends; full backend race/lint pass.

Emission still allocates locals for literals, so the captured PDF core is unchanged.
LITERAL_VALUES_REVIEW.md specifies the next required transition: a tagged literal
or stored operand consumed uniformly, with storage-only ownership/frame selection
and exact literal identity checks at continuations. Fake local-index tags and
heuristic expression rematerialization are excluded.
