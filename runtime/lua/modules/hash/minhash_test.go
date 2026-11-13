// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
)

type vectorPiece struct {
	Text  string `json:"text"`
	Times int    `json:"times"`
}

type vectorCase struct {
	Name       string        `json:"name"`
	Normalized string        `json:"normalized"`
	Pieces     []vectorPiece `json:"pieces"`
	Sketch     []uint32      `json:"sketch"`
	Bands      []string      `json:"bands"`
	Tokens     int           `json:"tokens"`
}

type vectorFile struct {
	Source string       `json:"source"`
	Domain string       `json:"domain"`
	Cases  []vectorCase `json:"cases"`
	Near   struct {
		A         []vectorPiece `json:"a"`
		B         []vectorPiece `json:"b"`
		EqualBins int           `json:"equal_bins"`
	} `json:"near"`
}

func loadVectors(t testing.TB) vectorFile {
	t.Helper()
	data, err := os.ReadFile("testdata/minhash_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func buildPieces(pieces []vectorPiece) string {
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(strings.Repeat(p.Text, p.Times))
	}
	return b.String()
}

func signatureOf(t testing.TB, c MinHashConfig, text string) MinHashResult {
	t.Helper()
	m, err := NewMinHasher(c, true)
	if err != nil {
		t.Fatal(err)
	}
	m.Write(text)
	return m.Sum()
}

func newMinHashState(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func TestMinHashGoldenVectors(t *testing.T) {
	v := loadVectors(t)
	c := DefaultMinHashConfig()
	c.Domain = v.Domain
	for _, vc := range v.Cases {
		t.Run(vc.Name, func(t *testing.T) {
			r := signatureOf(t, c, buildPieces(vc.Pieces))
			if r.Tokens != vc.Tokens {
				t.Fatalf("tokens = %d, want %d", r.Tokens, vc.Tokens)
			}
			if got := hex.EncodeToString(r.Normalized[:]); got != vc.Normalized {
				t.Fatalf("normalized = %s, want %s", got, vc.Normalized)
			}
			if vc.Sketch == nil {
				if r.Sketch != nil {
					t.Fatalf("sketch = %v, want none", r.Sketch)
				}
				return
			}
			if !slices.Equal(r.Sketch, vc.Sketch) {
				t.Fatalf("sketch = %v, want %v", r.Sketch, vc.Sketch)
			}
			keys, err := MinHashBands(r.Sketch, c)
			if err != nil {
				t.Fatal(err)
			}
			for i, key := range keys {
				if got := hex.EncodeToString(key[:]); got != vc.Bands[i] {
					t.Fatalf("band %d = %s, want %s", i, got, vc.Bands[i])
				}
			}
		})
	}
}

func TestMinHashLuaGoldenVectors(t *testing.T) {
	v := loadVectors(t)
	for _, vc := range v.Cases {
		t.Run(vc.Name, func(t *testing.T) {
			l := newMinHashState(t)
			l.SetGlobal("text", lua.LString(buildPieces(vc.Pieces)))
			l.SetGlobal("domain", lua.LString(v.Domain))
			if err := l.DoString(`result, err = hash.minhash.signature(text, { domain = domain })`); err != nil {
				t.Fatal(err)
			}
			if err := l.GetGlobal("err"); err != lua.LNil {
				t.Fatalf("signature error: %v", err)
			}
			result := l.GetGlobal("result").(*lua.LTable)
			if n, ok := result.RawGetString("tokens").(lua.LInteger); !ok || int(n) != vc.Tokens {
				t.Fatalf("tokens = %v, want %d", result.RawGetString("tokens"), vc.Tokens)
			}
			if got := result.RawGetString("normalized").String(); got != vc.Normalized {
				t.Fatalf("normalized = %s", got)
			}
			if vc.Sketch == nil {
				if result.RawGetString("sketch") != lua.LNil || result.RawGetString("bands") != lua.LNil {
					t.Fatal("sketch and bands must be absent without tokens")
				}
				return
			}
			sketch, err := DecodeMinHashSketch([]byte(result.RawGetString("sketch").String()))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(sketch, vc.Sketch) {
				t.Fatalf("sketch = %v", sketch)
			}
			bands := result.RawGetString("bands").(*lua.LTable)
			if bands.Len() != len(vc.Bands) {
				t.Fatalf("bands = %d", bands.Len())
			}
			for i, want := range vc.Bands {
				if got := bands.RawGetInt(i + 1).String(); got != want {
					t.Fatalf("band %d = %s, want %s", i, got, want)
				}
			}
		})
	}
}

func TestMinHashGoldenNearSimilarity(t *testing.T) {
	v := loadVectors(t)
	l := newMinHashState(t)
	l.SetGlobal("a", lua.LString(buildPieces(v.Near.A)))
	l.SetGlobal("b", lua.LString(buildPieces(v.Near.B)))
	l.SetGlobal("want", lua.LNumber(float64(v.Near.EqualBins)/128))
	if err := l.DoString(`
		local sa = assert(hash.minhash.sketch(a))
		local sb = assert(hash.minhash.sketch(b))
		local j, err = hash.minhash.similarity(sa, sb)
		assert(err == nil and j == want, "similarity " .. tostring(j))
		assert(hash.minhash.similarity(sa, sa) == 1, "self similarity is 1")
	`); err != nil {
		t.Fatal(err)
	}
}

// literalTokens transcribes the default tokenizer of spec.md.
func literalTokens(text []byte) [][]byte {
	isWord := func(b byte) bool {
		return b >= 0x80 || b == '_' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
	}
	var tokens [][]byte
	var current []byte
	for _, b := range text {
		if !isWord(b) {
			if current != nil {
				tokens = append(tokens, current)
				current = nil
			}
			continue
		}
		if b >= 'A' && b <= 'Z' {
			b += 32
		}
		current = append(current, b)
	}
	if current != nil {
		tokens = append(tokens, current)
	}
	return tokens
}

// literalSignature transcribes spec.md: token hashes, direct polynomial shingle
// hashes, bins and probe densification, and the normalized digest.
func literalSignature(tokens [][]byte, k, shingle int) MinHashResult {
	const m, p, c, stride = 2147483647, 1000003, 2654435761, 55
	out := MinHashResult{Tokens: len(tokens)}
	joined := make([]byte, 0)
	for i, token := range tokens {
		if i > 0 {
			joined = append(joined, ' ')
		}
		joined = append(joined, token...)
	}
	out.Normalized = sha256.Sum256(joined)
	if len(tokens) == 0 {
		return out
	}
	hashes := make([]uint64, len(tokens))
	for i, token := range tokens {
		h := uint32(0x811C9DC5)
		for _, b := range token {
			h *= 0x01000193
			h ^= uint32(b)
		}
		hashes[i] = uint64(h) % m
	}
	width := min(shingle, len(hashes))
	bins := make([]int64, k)
	for i := range bins {
		bins[i] = -1
	}
	for start := 0; start+width <= len(hashes); start++ {
		var s uint64
		for _, h := range hashes[start : start+width] {
			s = (s*p + h) % m
		}
		b, v := s%uint64(k), int64(s/uint64(k))
		if bins[b] < 0 || v < bins[b] {
			bins[b] = v
		}
	}
	out.Sketch = make([]uint32, k)
	for i, v := range bins {
		if v < 0 {
			base := (uint64(i+1) * c) % m
			for a := uint64(1); a <= uint64(k); a++ {
				if found := bins[(base+stride*a)%uint64(k)]; found >= 0 {
					v = found
					break
				}
			}
		}
		out.Sketch[i] = uint32(v)
	}
	return out
}

func randomText(r *rand.Rand, n int) []byte {
	alphabet := []byte("abcXYZ_09 \t\n.,;(){}=\"\x80\xc3\xa9\xff")
	vocabulary := []string{"local", "function", "return", "end", "State", "event", "if", "then", "x", "snake_case"}
	out := make([]byte, 0, n)
	for len(out) < n {
		switch r.IntN(12) {
		case 0:
			out = append(out, strings.Repeat("Q", r.IntN(200))...)
		case 1, 2, 3:
			out = append(out, alphabet[r.IntN(len(alphabet))])
		default:
			out = append(out, vocabulary[r.IntN(len(vocabulary))]...)
			out = append(out, alphabet[10+r.IntN(10)])
		}
	}
	return out
}

func randomConfig(r *rand.Rand) MinHashConfig {
	c := DefaultMinHashConfig()
	c.Shingle = 1 + r.IntN(8)
	for {
		c.K = 1 + r.IntN(300)
		if gcd(uint64(c.K), minhashStride) == 1 {
			break
		}
	}
	c.Bands, c.Rows = c.K, 1
	return c
}

func equalResults(a, b MinHashResult) bool {
	return a.Tokens == b.Tokens && a.Normalized == b.Normalized && slices.Equal(a.Sketch, b.Sketch) && (a.Sketch == nil) == (b.Sketch == nil)
}

func TestMinHashMatchesLiteralSpecification(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 3000; i++ {
		c := DefaultMinHashConfig()
		if i%2 == 1 {
			c = randomConfig(r)
		}
		text := randomText(r, r.IntN(800))
		want := literalSignature(literalTokens(text), c.K, c.Shingle)
		got := signatureOf(t, c, string(text))
		if !equalResults(got, want) {
			t.Fatalf("case %d config %+v text %q:\n got %+v\nwant %+v", i, c, text, got, want)
		}
	}
}

func TestMinHashLongTokenStaysWhole(t *testing.T) {
	c := DefaultMinHashConfig()
	for _, n := range []int{32767, 32768, 32769, 65536, 70001, 1 << 20} {
		text := "head " + strings.Repeat("Ab_\xc3", n/4) + " tail end"
		want := literalSignature(literalTokens([]byte(text)), c.K, c.Shingle)
		if want.Tokens != 4 {
			t.Fatalf("literal tokens = %d", want.Tokens)
		}
		if got := signatureOf(t, c, text); !equalResults(got, want) {
			t.Fatalf("token of %d bytes: got %+v, want %+v", n, got, want)
		}
	}
}

func TestMinHashStreamingMatchesOneShot(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 2000; i++ {
		c := DefaultMinHashConfig()
		if i%2 == 1 {
			c = randomConfig(r)
		}
		text := string(randomText(r, r.IntN(1500)))
		want := signatureOf(t, c, text)
		m, err := NewMinHasher(c, true)
		if err != nil {
			t.Fatal(err)
		}
		for rest := text; rest != ""; {
			n := r.IntN(min(len(rest), 64) + 1)
			m.Write(rest[:n])
			rest = rest[n:]
			if r.IntN(8) == 0 {
				m.Sum()
			}
		}
		if got := m.Sum(); !equalResults(got, want) {
			t.Fatalf("case %d config %+v: streamed %+v, one-shot %+v", i, c, got, want)
		}
		m.Reset()
		m.Write(text)
		if got := m.Sum(); !equalResults(got, want) {
			t.Fatalf("case %d: after reset %+v, want %+v", i, got, want)
		}
	}
}

func TestMinHashStreamingMemoryIsBounded(t *testing.T) {
	m, err := NewMinHasher(DefaultMinHashConfig(), true)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(13, 14))
	for range 256 {
		m.Write(string(randomText(r, 8192)))
		m.Write(strings.Repeat("Z", 5000))
	}
	m.Sum()
	if cap(m.pending) > normFlush+1 || len(m.bins) != MinHashDefaultK || len(m.window) != MinHashDefaultShingle {
		t.Fatalf("state grew: pending cap %d, bins %d, window %d", cap(m.pending), len(m.bins), len(m.window))
	}
}

func TestMinHashTokensMatchText(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 1000; i++ {
		c := randomConfig(r)
		text := randomText(r, r.IntN(600))
		want := signatureOf(t, c, string(text))
		m, err := NewMinHasher(c, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range literalTokens(text) {
			m.WriteToken(string(token))
		}
		if got := m.Sum(); !equalResults(got, want) {
			t.Fatalf("case %d: tokens %+v, text %+v", i, got, want)
		}
	}
}

func TestMinHashSimilarityTracksJaccard(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	words := make([]string, 4000)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", r.Uint32())
	}
	c := DefaultMinHashConfig()
	a := signatureOf(t, c, strings.Join(words[:3000], " "))
	b := signatureOf(t, c, strings.Join(words[1000:], " "))
	j, err := MinHashSimilarity(a.Sketch, b.Sketch)
	if err != nil {
		t.Fatal(err)
	}
	// The shingle sets share 1996 of 3996 distinct shingles: J ~= 0.50.
	if j < 0.35 || j > 0.65 {
		t.Fatalf("similarity = %v, want about 0.50", j)
	}
}

func TestMinHashLuaAPI(t *testing.T) {
	l := newMinHashState(t)
	if err := l.DoString(`
		local text = "local function fold(state, event) return apply(state, event) end"
		local sig = assert(hash.minhash.signature(text))
		assert(sig.tokens == 10 and #sig.sketch == 512 and #sig.bands == 16, "signature shape")
		assert(#sig.normalized == 64, "normalized is hex sha256")
		assert(sig.normalized == hash.sha256("local function fold state event return apply state event end"))

		assert(hash.minhash.sketch(text) == sig.sketch, "sketch agrees with signature")
		local keys = assert(hash.minhash.bands(sig.sketch))
		for i = 1, 16 do assert(keys[i] == sig.bands[i], "band " .. i) end
		local other = assert(hash.minhash.bands(sig.sketch, { domain = "other" }))
		assert(other[1] ~= keys[1], "domain separates band keys")
		local wide = assert(hash.minhash.bands(sig.sketch, { bands = 32, rows = 4 }))
		assert(#wide == 32 and #wide[1] == 16, "32 bands of 16 hex digits")

		local tokens = { "local", "function", "fold", "state", "event", "return", "apply", "state", "event", "end" }
		local from_tokens = assert(hash.minhash.signature(tokens))
		assert(from_tokens.sketch == sig.sketch and from_tokens.normalized == sig.normalized, "tokens equal text")
		assert(from_tokens.tokens == 10)

		local small = assert(hash.minhash.sketch(text, { k = 64, shingle = 3.0 }))
		assert(#small == 256, "k selects the sketch size")

		local empty = assert(hash.minhash.signature(" ,;"))
		assert(empty.tokens == 0 and empty.sketch == nil and empty.bands == nil, "no tokens")
		assert(empty.normalized == hash.sha256(""), "normalized of no tokens")
		assert(hash.minhash.sketch({}) == "", "empty token array")

		local m = assert(hash.minhash.new())
		assert(tostring(m) == "hash.MinHasher(k=128, shingle=5)")
		assert(m:update("local func"))
		assert(m:update("tion fold(state, ev"))
		local partial = m:sum()
		assert(partial.tokens == 5, "partial token counts at sum")
		assert(m:update("ent) return apply(state, event) end"))
		local streamed = m:sum()
		assert(streamed.sketch == sig.sketch and streamed.normalized == sig.normalized, "stream equals one-shot")
		m:reset()
		assert(m:sum().tokens == 0, "reset clears the state")
	`); err != nil {
		t.Fatal(err)
	}
}

func TestMinHashLuaInvalidArguments(t *testing.T) {
	cases := map[string]string{
		"input not a string or array": `return hash.minhash.sketch(1)`,
		"input with a hash part":      `return hash.minhash.sketch({ "a", x = "b" })`,
		"token not a string":          `return hash.minhash.sketch({ "a", 2 })`,
		"empty token":                 `return hash.minhash.signature({ "a", "" })`,
		"options not a table":         `return hash.minhash.sketch("a", 1)`,
		"unknown option":              `return hash.minhash.sketch("a", { width = 5 })`,
		"band option on sketch":       `return hash.minhash.sketch("a", { bands = 16 })`,
		"fixed constant as option":    `return hash.minhash.signature("a", { modulus = 7 })`,
		"fractional integer":          `return hash.minhash.sketch("a", { k = 1.5 })`,
		"negative integer":            `return hash.minhash.sketch("a", { shingle = -1 })`,
		"string integer":              `return hash.minhash.sketch("a", { k = "128" })`,
		"k shares a factor with 55":   `return hash.minhash.sketch("a", { k = 100 })`,
		"k zero":                      `return hash.minhash.sketch("a", { k = 0 })`,
		"k too large":                 `return hash.minhash.sketch("a", { k = 65537 })`,
		"shingle too large":           `return hash.minhash.sketch("a", { shingle = 1025 })`,
		"bands times rows not k":      `return hash.minhash.signature("a", { k = 64 })`,
		"domain not a string":         `return hash.minhash.signature("a", { domain = 1 })`,
		"bands of a short sketch":     `return hash.minhash.bands("abc")`,
		"bands not a string":          `return hash.minhash.bands(1)`,
		"bands not matching sketch":   `return hash.minhash.bands(hash.minhash.sketch("a b"), { bands = 3, rows = 3 })`,
		"similarity of unequal sizes": `return hash.minhash.similarity(hash.minhash.sketch("a b"), hash.minhash.sketch("a b", { k = 64 }))`,
		"similarity of empty":         `return hash.minhash.similarity("", "")`,
		"new with invalid banding":    `return hash.minhash.new({ k = 64 })`,
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			l := newMinHashState(t)
			if err := l.DoString(`
				local result, err = (function() ` + code + ` end)()
				assert(result == nil, "result must be nil")
				assert(err ~= nil, "error expected")
				assert(err:kind() == errors.INVALID, "kind " .. tostring(err:kind()))
				assert(err:retryable() == false, "not retryable")
			`); err != nil {
				t.Fatal(err)
			}
		})
	}
	l := newMinHashState(t)
	if err := l.DoString(`
		local m = assert(hash.minhash.new())
		local ok, err = m:update(7)
		assert(ok == false and err:kind() == errors.INVALID, "update refuses a non-string")
	`); err != nil {
		t.Fatal(err)
	}
}

func TestMinHashSketchEncoding(t *testing.T) {
	sketch := []uint32{0, 1, 0xFFFFFFFF, 0x80000000, 7206114}
	encoded := EncodeMinHashSketch(sketch)
	if hex.EncodeToString(encoded[8:12]) != "ffffffff" || len(encoded) != 20 {
		t.Fatalf("encoding is not 32-bit big-endian: %x", encoded)
	}
	decoded, err := DecodeMinHashSketch(encoded)
	if err != nil || !slices.Equal(decoded, sketch) {
		t.Fatalf("decoded = %v, %v", decoded, err)
	}
}

// codeRecord renders a deterministic code-like record of about n bytes.
func codeRecord(r *rand.Rand, n int) string {
	identifiers := []string{"state", "event", "apply", "batch", "ops", "result", "err", "self", "items", "count", "value", "key", "index", "Context", "Handler"}
	var b strings.Builder
	for b.Len() < n {
		name := identifiers[r.IntN(len(identifiers))]
		other := identifiers[r.IntN(len(identifiers))]
		switch r.IntN(4) {
		case 0:
			fmt.Fprintf(&b, "local function %s_%d(%s, %s)\n", name, r.IntN(100), other, name)
		case 1:
			fmt.Fprintf(&b, "    if %s.%s == \"%s.%d\" then return %s(%s) end\n", name, other, other, r.IntN(10), other, name)
		case 2:
			fmt.Fprintf(&b, "    -- Apply %s to the %s and return the %s.\n", other, name, other)
		default:
			fmt.Fprintf(&b, "    %s[%d] = %s + %d\nend\n", name, r.IntN(1000), other, r.IntN(1<<16))
		}
	}
	return b.String()
}

func BenchmarkMinHashSignature5KB(b *testing.B) {
	r := rand.New(rand.NewPCG(9, 10))
	records := make([]string, 64)
	for i := range records {
		records[i] = codeRecord(r, 5*1024)
	}
	c := DefaultMinHashConfig()
	b.SetBytes(int64(len(records[0])))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, err := NewMinHasher(c, true)
		if err != nil {
			b.Fatal(err)
		}
		m.Write(records[i%len(records)])
		if _, err := MinHashBands(m.Sum().Sketch, c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMinHashStream1MB(b *testing.B) {
	r := rand.New(rand.NewPCG(11, 12))
	text := codeRecord(r, 1<<20)
	c := DefaultMinHashConfig()
	m, err := NewMinHasher(c, true)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Reset()
		for rest := text; rest != ""; {
			n := min(len(rest), 32*1024)
			m.Write(rest[:n])
			rest = rest[n:]
		}
		m.Sum()
	}
}

func TestSpecExampleRuns(t *testing.T) {
	spec, err := os.ReadFile("spec.md")
	if err != nil {
		t.Fatal(err)
	}
	_, example, ok := strings.Cut(strings.ReplaceAll(string(spec), "\r\n", "\n"), "## Example")
	if !ok {
		t.Fatal("spec.md has no Example section")
	}
	_, code, ok := strings.Cut(example, "```lua\n")
	if !ok {
		t.Fatal("Example section has no Lua block")
	}
	code, _, _ = strings.Cut(code, "```")
	l := newMinHashState(t)
	if err := l.DoString(`function require(name) return ({ hash = hash })[name] end`); err != nil {
		t.Fatal(err)
	}
	if err := l.DoString(code); err != nil {
		t.Fatal(err)
	}
}
