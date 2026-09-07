# Provenance for internal validation fixtures

This directory preserves source material for the checked-in W1 validation
artifacts. It intentionally contains no compiled output or Cargo build cache.

| fixture | checked-in SHA-256 | retained source provenance | confidence |
| --- | --- | --- | --- |
| `actor_direct_poll.wasm` | `31a2ef2c8306423068e23b59b1885f3a62e2f090c4382f5790850a3bb26e3f3c` | `actor_direct_poll/` preserves the source, manifest, lockfile, WIT input, and build metadata retained alongside an artifact matching the checked-in binary. | The retained artifact is byte-identical to W1's checked-in fixture. |
| `mixed.wasm` | `ff93415d494e7780679ab5ef44b8a6a808e6189b51612722ea6f98836fe67880` | `mixed/` preserves the Rust source and world retained alongside an artifact matching the checked-in binary. The original Cargo project is not retained. This directory supplies a minimal manifest and the compatible locked dependency set from `variant-listener-mixed-poll-try-empty`; its lockfile matches W1's retained TCP fixture lockfile. | The retained artifact is byte-identical; source-to-byte reproducibility has not been demonstrated. |

`actor_direct_poll` was built in the retained workspace with Rust `1.96.0`
(commit `ac68faa20c58cbccd01ee7208bf3b6e93a7d7f96`), Cargo `1.96.0`, target
`wasm32-wasip2`, release profile `opt-level = 2`, and `panic = "abort"`.
The `mixed` binary has no retained build invocation or original manifest, so no
compiler provenance is claimed for it.

Run `./verify-artifacts.sh /path/to/w1` to validate the checked-in binaries.
This verifies artifact identity only; it does not build. To produce fresh
artifacts, run `./rebuild.sh /output/directory` with the `wasm32-wasip2` target
installed. Compare the printed hashes as a diagnostic. Do not infer byte
identity without a controlled rebuild and a matching hash.

The adjacent `reused-result.wat` and `two-core.wat` are readable disassemblies
of the small synthetic fixtures. Parsing each with `wasm-tools parse` reproduced
the checked-in Wasm bytes exactly during validation. They are not claimed as
the original authored sources.
