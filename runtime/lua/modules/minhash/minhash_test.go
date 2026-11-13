// SPDX-License-Identifier: MPL-2.0

package minhash

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
)

type piece struct {
	Text  string `json:"text"`
	Times int    `json:"times"`
}

type vectorCase struct {
	Name             string   `json:"name"`
	Pieces           []piece  `json:"pieces"`
	NormalizedSHA256 string   `json:"normalized_sha256"`
	Sketch           []uint32 `json:"sketch"`
	Bands            []string `json:"bands"`
	Words            int      `json:"words"`
}

type vectorFile struct {
	Source string       `json:"source"`
	Domain string       `json:"domain"`
	Cases  []vectorCase `json:"cases"`
	Near   struct {
		A         []piece `json:"a"`
		B         []piece `json:"b"`
		EqualBins int     `json:"equal_bins"`
	} `json:"near"`
}

func loadVectors(t testing.TB) vectorFile {
	t.Helper()
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func build(pieces []piece) string {
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(strings.Repeat(p.Text, p.Times))
	}
	return b.String()
}

func TestGoldenVectors(t *testing.T) {
	v := loadVectors(t)
	lsh := LSH{Domain: v.Domain, Bands: 16, Rows: 8}
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			result, err := Signature(build(c.Pieces), DefaultParams())
			if err != nil {
				t.Fatal(err)
			}
			sketch := result.Sketch
			if result.Words != c.Words {
				t.Fatalf("words = %d, want %d", result.Words, c.Words)
			}
			if got := hex.EncodeToString(result.Normalized[:]); got != c.NormalizedSHA256 {
				t.Fatalf("normalized sha256 = %s, want %s", got, c.NormalizedSHA256)
			}
			if c.Sketch == nil {
				if sketch != nil {
					t.Fatalf("sketch = %v, want none", sketch)
				}
				return
			}
			if fmt.Sprint(sketch) != fmt.Sprint(c.Sketch) {
				t.Fatalf("sketch = %v, want %v", sketch, c.Sketch)
			}
			keys, err := lsh.Keys(sketch)
			if err != nil {
				t.Fatal(err)
			}
			for i, key := range keys {
				if got := hex.EncodeToString(key[:]); got != c.Bands[i] {
					t.Fatalf("band %d = %s, want %s", i, got, c.Bands[i])
				}
			}
		})
	}
}

func TestGoldenNearEstimate(t *testing.T) {
	v := loadVectors(t)
	_, a, err := Sketch(build(v.Near.A), DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := Sketch(build(v.Near.B), DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Estimate(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if want := float64(v.Near.EqualBins) / 128; got != want {
		t.Fatalf("estimate = %v, want %v", got, want)
	}
}

// referenceSketch transcribes the specification literally: segments, tokens, direct
// polynomial shingle hashes, bins and probe densification.
func referenceSketch(text []byte, p Params) (int, []uint32) {
	isWord := func(b byte) bool {
		return b >= 0x80 || b == '_' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
	}
	var tokens [][]byte
	for pos := 0; pos < len(text); {
		end := len(text)
		if end-pos > p.Segment {
			end = pos + p.Segment
			k := end - 1
			for k >= pos && isWord(text[k]) {
				k--
			}
			if k >= pos {
				end = k + 1
			}
		}
		var current []byte
		for _, b := range text[pos:end] {
			if isWord(b) {
				if b >= 'A' && b <= 'Z' {
					b += 32
				}
				current = append(current, b)
			} else if current != nil {
				tokens = append(tokens, current)
				current = nil
			}
		}
		if current != nil {
			tokens = append(tokens, current)
		}
		pos = end
	}
	if len(tokens) == 0 {
		return 0, nil
	}
	hashes := make([]uint64, len(tokens))
	for i, token := range tokens {
		h := uint32(0x811C9DC5)
		for _, b := range token {
			h *= 0x01000193
			h ^= uint32(b)
		}
		hashes[i] = uint64(h) % p.Modulus
	}
	width := min(p.Shingle, len(hashes))
	k := uint64(p.K)
	bins := make([]int64, p.K)
	for i := range bins {
		bins[i] = -1
	}
	for start := 0; start+width <= len(hashes); start++ {
		var s uint64
		for _, h := range hashes[start : start+width] {
			s = (s*p.Base + h) % p.Modulus
		}
		b, v := s%k, int64(s/k)
		if bins[b] < 0 || v < bins[b] {
			bins[b] = v
		}
	}
	out := make([]uint32, p.K)
	for i := range bins {
		v := bins[i]
		if v < 0 {
			base := (uint64(i+1) * p.Probe) % p.Modulus
			for a := uint64(1); a <= k; a++ {
				if found := bins[(base+p.Stride*a)%k]; found >= 0 {
					v = found
					break
				}
			}
		}
		out[i] = uint32(v)
	}
	return len(tokens), out
}

func randomText(r *rand.Rand, n int) []byte {
	alphabet := []byte("abcXYZ_09 \t\n.,;(){}=\"\x80\xc3\xa9\xff")
	vocabulary := []string{"local", "function", "return", "end", "State", "event", "if", "then", "x"}
	out := make([]byte, 0, n)
	for len(out) < n {
		if r.IntN(3) == 0 {
			out = append(out, alphabet[r.IntN(len(alphabet))])
			continue
		}
		out = append(out, vocabulary[r.IntN(len(vocabulary))]...)
		out = append(out, alphabet[9+r.IntN(10)])
	}
	return out
}

func randomParams(r *rand.Rand) Params {
	p := Params{
		Shingle: 1 + r.IntN(8),
		K:       1 + r.IntN(300),
		Segment: 1 + r.IntN(64),
		Modulus: 2 + r.Uint64N(MaxModulus-1),
		Base:    1 + r.Uint64N(MaxBase),
		Probe:   r.Uint64N(MaxProbe + 1),
	}
	for {
		p.Stride = 1 + r.Uint64N(MaxStride)
		if gcd(p.Stride, uint64(p.K)) == 1 {
			return p
		}
	}
}

func TestSketchMatchesLiteralSpecification(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 2000; i++ {
		p := DefaultParams()
		if i%2 == 1 {
			p = randomParams(r)
		}
		text := randomText(r, r.IntN(600))
		wantWords, want := referenceSketch(text, p)
		words, got, err := Sketch(string(text), p)
		if err != nil {
			t.Fatal(err)
		}
		if words != wantWords || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("case %d params %+v text %q:\n got %d %v\nwant %d %v", i, p, text, words, got, wantWords, want)
		}
	}
}

func TestSketchProperties(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 500; i++ {
		p := randomParams(r)
		text := randomText(r, 1+r.IntN(400))
		_, sketch, err := Sketch(string(text), p)
		if err != nil {
			t.Fatal(err)
		}
		if sketch == nil {
			continue
		}
		if len(sketch) != p.K {
			t.Fatalf("sketch has %d bins, want %d", len(sketch), p.K)
		}
		bound := p.Modulus / uint64(p.K)
		for _, v := range sketch {
			if uint64(v) > bound {
				t.Fatalf("bin value %d exceeds modulus/k %d", v, bound)
			}
		}
		same, err := Estimate(sketch, sketch)
		if err != nil || same != 1 {
			t.Fatalf("self estimate = %v, %v", same, err)
		}
	}
}

func TestSketchIgnoresASCIICaseAndSeparatorChoice(t *testing.T) {
	a := "Local Function FOLD(state, event) return APPLY(state) end"
	b := "local\tfunction fold state;event\n\nreturn apply{state}...end"
	wa, sa, err := Sketch(a, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	wb, sb, err := Sketch(b, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if wa != wb || fmt.Sprint(sa) != fmt.Sprint(sb) {
		t.Fatalf("sketches differ: %d %v / %d %v", wa, sa, wb, sb)
	}
	ra, _ := Signature(a, DefaultParams())
	rb, _ := Signature(b, DefaultParams())
	if ra.Normalized != rb.Normalized {
		t.Fatal("normalized digests differ")
	}
}

func TestSketchKeepsNonASCIICase(t *testing.T) {
	_, a, _ := Sketch("caf\xc3\xa9 one two three four", DefaultParams())
	_, b, _ := Sketch("caf\xc3\x89 one two three four", DefaultParams())
	if fmt.Sprint(a) == fmt.Sprint(b) {
		t.Fatal("bytes >= 0x80 must keep their value")
	}
}

func TestEstimateTracksJaccard(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	words := make([]string, 4000)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", r.Uint32())
	}
	a := strings.Join(words[:3000], " ")
	b := strings.Join(words[1000:], " ")
	_, sa, _ := Sketch(a, DefaultParams())
	_, sb, _ := Sketch(b, DefaultParams())
	got, err := Estimate(sa, sb)
	if err != nil {
		t.Fatal(err)
	}
	// The shingle sets share 1996 of 3996 distinct shingles: J ~= 0.50.
	if got < 0.35 || got > 0.65 {
		t.Fatalf("estimate = %v, want about 0.50", got)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	sketch := []uint32{0, 1, 0xFFFFFFFF, 0x80000000, 7206114}
	encoded := Encode(sketch)
	if len(encoded) != 4*len(sketch) {
		t.Fatalf("encoded length = %d", len(encoded))
	}
	if hex.EncodeToString(encoded[8:12]) != "ffffffff" {
		t.Fatalf("encoding is not big-endian: %x", encoded)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(decoded) != fmt.Sprint(sketch) {
		t.Fatalf("decoded = %v", decoded)
	}
	if _, err := Decode(encoded[:5]); err == nil {
		t.Fatal("decode accepts a length that is not a multiple of 4")
	}
	if _, err := Decode(nil); err == nil {
		t.Fatal("decode accepts an empty sketch")
	}
}

func TestEstimateRejectsMismatchedSketches(t *testing.T) {
	if _, err := Estimate([]uint32{1, 2}, []uint32{1}); err == nil {
		t.Fatal("estimate accepts sketches of different sizes")
	}
	if _, err := Estimate(nil, nil); err == nil {
		t.Fatal("estimate accepts empty sketches")
	}
}

func TestLSHValidation(t *testing.T) {
	sketch := make([]uint32, 128)
	cases := []LSH{
		{Bands: 16, Rows: 4},
		{Bands: 0, Rows: 8},
		{Bands: 16, Rows: 0},
	}
	for _, l := range cases {
		if _, err := l.Keys(sketch); err == nil {
			t.Fatalf("keys accepts %+v for 128 bins", l)
		}
	}
	keys, err := LSH{Domain: "d", Bands: 32, Rows: 4}.Keys(sketch)
	if err != nil || len(keys) != 32 {
		t.Fatalf("keys = %d, %v", len(keys), err)
	}
}

func TestParamsValidation(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*Params){
		"shingle zero":        func(p *Params) { p.Shingle = 0 },
		"shingle too large":   func(p *Params) { p.Shingle = MaxShingle + 1 },
		"k zero":              func(p *Params) { p.K = 0 },
		"k too large":         func(p *Params) { p.K = MaxK + 1 },
		"segment zero":        func(p *Params) { p.Segment = 0 },
		"modulus one":         func(p *Params) { p.Modulus = 1 },
		"modulus too large":   func(p *Params) { p.Modulus = MaxModulus + 1 },
		"base zero":           func(p *Params) { p.Base = 0 },
		"base too large":      func(p *Params) { p.Base = MaxBase + 1 },
		"probe too large":     func(p *Params) { p.Probe = MaxProbe + 1 },
		"stride zero":         func(p *Params) { p.Stride = 0 },
		"stride too large":    func(p *Params) { p.Stride = MaxStride + 1 },
		"stride shares k":     func(p *Params) { p.Stride = 64 },
		"stride shares odd k": func(p *Params) { p.K = 33; p.Stride = 55 },
	}
	for name, mutate := range mutations {
		p := DefaultParams()
		mutate(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: params %+v accepted", name, p)
		}
		if _, _, err := Sketch("a b c", p); err == nil {
			t.Errorf("%s: sketch accepts invalid params", name)
		}
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

func benchmarkRecords() ([]string, int64) {
	r := rand.New(rand.NewPCG(7, 8))
	records := make([]string, 64)
	total := 0
	for i := range records {
		records[i] = codeRecord(r, 5*1024)
		total += len(records[i])
	}
	return records, int64(total / len(records))
}

func BenchmarkSignature5KB(b *testing.B) {
	records, size := benchmarkRecords()
	p := DefaultParams()
	lsh := DefaultLSH()
	b.SetBytes(size)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := Signature(records[i%len(records)], p)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := lsh.Keys(result.Sketch); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSketch5KB(b *testing.B) {
	records, size := benchmarkRecords()
	p := DefaultParams()
	b.SetBytes(size)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Sketch(records[i%len(records)], p); err != nil {
			b.Fatal(err)
		}
	}
}
