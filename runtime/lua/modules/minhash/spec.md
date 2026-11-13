<!-- SPDX-License-Identifier: MPL-2.0 -->

# minhash

MinHash sketches of text for near-duplicate detection: one-permutation hashing with
optimal densification over word shingles, LSH band keys and Jaccard estimates. Encoding,
deterministic. Every output is bit-exact across platforms and implementations of the
algorithm below; no value passes through a float except the estimate.

## Loading

```lua
local minhash = require("minhash")
```

## Algorithm

Parameters (option name, default):

| Option | Default | Meaning |
|--------|---------|---------|
| shingle | 5 | tokens per shingle, 1..1024 |
| k | 128 | sketch bins, 1..65536 |
| segment | 32768 | bytes per tokenization segment, >= 1 |
| modulus | 2147483647 | field M of token and shingle hashes, 2..2^31-1 |
| base | 1000003 | polynomial base P of shingle hashes, 1..2^32-1 |
| probe | 2654435761 | densification probe multiplier C, 0..2^32-1 |
| stride | 55 | densification probe step, 1..2^32-1, coprime with k |
| bands | 16 | LSH bands; bands * rows must equal k |
| rows | 8 | bins per band |
| domain | "minhash.lsh.v1" | domain string framed into every band key |

1. **Segments.** The text (bytes) is cut into segments of at most `segment` bytes. A
   segment that is not the tail of the text ends just after the last separator byte of
   its window, or at the window end when the window holds no separator (a longer token
   is then cut).
2. **Tokens.** Within a segment, a token is a maximal run of word bytes: `[A-Za-z0-9_]`
   and every byte `>= 0x80`. ASCII letters are lowered; other bytes keep their value.
   Every other byte is a separator. `words` is the token count; the normalized text is
   the tokens joined by one space.
3. **Shingles.** A token hashes to its 32-bit FNV-1 hash (offset `0x811C9DC5`, prime
   `0x01000193`, multiply then xor) modulo M. A shingle is `shingle` consecutive tokens,
   or one shingle of all tokens when there are fewer; its hash is
   `((t1*P + t2)*P + ... + tn) mod M`.
4. **Sketch.** Bin `s mod k` keeps the least `s div k` over all shingle hashes `s`. An
   empty bin `i` (0-based) takes the value the first non-empty bin of the probe sequence
   `j(a) = (((i+1)*C mod M) + stride*a) mod k`, `a = 1..k`, held before densification.
   A text without tokens has no sketch.
5. **Bands.** Band `b` (0-based) covers bins `rows*b .. rows*b+rows-1`. Its key is the
   first 8 bytes, as 16 lowercase hex digits, of
   `sha256(frame(domain, decimal(b), the band's bins as 32-bit big-endian words))`,
   where `frame` concatenates each field prefixed by its decimal byte length and `:`.
6. **Estimate.** The Jaccard similarity of two texts' shingle sets is estimated by the
   share of equal bins of their sketches.

A sketch crosses the Lua boundary as `4*k` bytes: its bins as consecutive 32-bit
big-endian words.

## Functions

### sketch(text: string, options?: SketchOptions) → string, error

Computes the sketch of a text.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| text | string | yes | - | Text bytes |
| options | table | no | - | `shingle`, `k`, `segment`, `modulus`, `base`, `probe`, `stride` |

**Returns:**
- Success: `4*k` bytes, or `""` when the text has no tokens
- Error: `nil, error` - structured error

### signature(text: string, options?: SignatureOptions) → Signature, error

Computes everything a text is compared by in one pass.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| text | string | yes | - | Text bytes |
| options | table | no | - | sketch options plus `bands`, `rows`, `domain` |

**Returns:**
- Success: table
  - `words: integer` - token count
  - `normalized_sha256: string` - hex sha256 of the normalized text
  - `sketch: string?` - as `sketch` answers it; absent when the text has no tokens
  - `bands: {string}?` - band keys in band order; absent when the text has no tokens
- Error: `nil, error` - structured error

### bands(sketch: string, options?: BandOptions) → {string}, error

Computes the LSH band keys of a sketch.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sketch | string | yes | - | Sketch bytes |
| options | table | no | - | `bands`, `rows`, `domain`; bands * rows must equal the sketch's bins |

**Returns:**
- Success: array of 16-digit hex keys
- Error: `nil, error` - structured error

### estimate(a: string, b: string) → number, error

Estimates the Jaccard similarity of the texts two sketches were computed from: equal
bins divided by bins.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| a | string | yes | - | Sketch bytes |
| b | string | yes | - | Sketch bytes of the same size |

**Returns:**
- Success: number in 0..1
- Error: `nil, error` - structured error

## Errors

| Condition | Kind | Retryable |
|-----------|------|-----------|
| text or sketch not a string | errors.INVALID | no |
| options not a table, unknown option, option of the wrong type | errors.INVALID | no |
| option out of range, stride not coprime with k | errors.INVALID | no |
| bands * rows not equal to the bins | errors.INVALID | no |
| sketch empty or not a whole number of 32-bit words | errors.INVALID | no |
| sketches of different sizes | errors.INVALID | no |

**Possible kinds:** `errors.INVALID`

## Example

```lua
local minhash = require("minhash")

local a = minhash.signature("local function fold(state, event) return apply(state, event) end")
local b = minhash.signature("local function fold(state, event) return apply(state, other) end")
assert(a.words == 10 and #a.sketch == 512 and #a.bands == 16)

-- Texts sharing a band key are candidates; the estimate confirms them.
local similarity = minhash.estimate(a.sketch, b.sketch)
assert(similarity > 0.3 and similarity < 1)

-- Case and separators do not change the tokens.
local same = minhash.sketch("LOCAL function   FOLD; state event return apply state event end")
assert(same == a.sketch)
```
