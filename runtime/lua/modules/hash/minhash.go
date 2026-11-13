// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"strconv"
)

// MinHash defaults and fixed constants. The constants are part of the
// specification in spec.md and are not configurable per call.
const (
	MinHashDefaultK       = 128
	MinHashDefaultShingle = 5
	MinHashDefaultBands   = 16
	MinHashDefaultRows    = 8
	MinHashDefaultDomain  = "minhash.lsh.v1"

	MinHashMaxK       = 65536
	MinHashMaxShingle = 1024

	minhashModulus = 2147483647 // M = 2^31-1, field of token and shingle hashes
	minhashBase    = 1000003    // P, polynomial base of shingle hashes
	minhashProbe   = 2654435761 // C, densification probe multiplier
	minhashStride  = 55         // densification probe step

	fnv32Offset = 0x811C9DC5
	fnv32Prime  = 0x01000193
	emptyBin    = ^uint32(0)
	normFlush   = 1024
)

// MinHashConfig selects the sketch size, the shingle width and the LSH banding.
type MinHashConfig struct {
	Domain  string
	K       int
	Shingle int
	Bands   int
	Rows    int
}

// DefaultMinHashConfig answers the configuration of the specification.
func DefaultMinHashConfig() MinHashConfig {
	return MinHashConfig{
		K:       MinHashDefaultK,
		Shingle: MinHashDefaultShingle,
		Bands:   MinHashDefaultBands,
		Rows:    MinHashDefaultRows,
		Domain:  MinHashDefaultDomain,
	}
}

// validateSketch answers why the sketch parameters of c cannot be used, or nil.
func (c MinHashConfig) validateSketch() error {
	switch {
	case c.K < 1 || c.K > MinHashMaxK:
		return fmt.Errorf("k must be in 1..%d", MinHashMaxK)
	case gcd(uint64(c.K), minhashStride) != 1:
		return fmt.Errorf("k must be coprime with the probe stride %d", minhashStride)
	case c.Shingle < 1 || c.Shingle > MinHashMaxShingle:
		return fmt.Errorf("shingle must be in 1..%d", MinHashMaxShingle)
	}
	return nil
}

// validateBands answers why the banding of c cannot cover a sketch of k bins, or nil.
func (c MinHashConfig) validateBands(k int) error {
	if c.Bands < 1 || c.Rows < 1 {
		return errors.New("bands and rows must be positive")
	}
	if c.Bands*c.Rows != k {
		return fmt.Errorf("bands * rows must equal the sketch size %d", k)
	}
	return nil
}

// Validate answers why c cannot be used for signatures, or nil.
func (c MinHashConfig) Validate() error {
	if err := c.validateSketch(); err != nil {
		return err
	}
	return c.validateBands(c.K)
}

func gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// wordByte maps a byte to its lowered value when it belongs to a token and to 0
// when it is a separator.
var wordByte = func() (t [256]byte) {
	for c := 0; c < 256; c++ {
		switch {
		case c >= 'A' && c <= 'Z':
			t[c] = byte(c + 32)
		case c >= 0x80 || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z'):
			t[c] = byte(c)
		}
	}
	return t
}()

// MinHasher accumulates tokens and keeps only what the sketch needs: the hash of
// the token in progress, the last Shingle token hashes and the K bins.
type MinHasher struct {
	norm    hash.Hash // sha256 of the tokens joined by one space; nil when not kept
	pending []byte    // lowered token bytes not yet written to norm
	bins    []uint32
	window  []uint64 // ring of the last Shingle token hashes
	lead    uint64   // P^(Shingle-1) mod M
	rolling uint64   // hash of the last min(count, Shingle) tokens
	count   int      // completed tokens
	token   uint32   // FNV-1 state of the token in progress
	inToken bool
	shingle int
}

// NewMinHasher answers an empty hasher; normalize keeps the normalized digest.
func NewMinHasher(c MinHashConfig, normalize bool) (*MinHasher, error) {
	if err := c.validateSketch(); err != nil {
		return nil, err
	}
	m := &MinHasher{
		bins:    make([]uint32, c.K),
		window:  make([]uint64, c.Shingle),
		shingle: c.Shingle,
		lead:    1,
	}
	for range c.Shingle - 1 {
		m.lead = m.lead * minhashBase % minhashModulus
	}
	if normalize {
		m.norm = sha256.New()
		m.pending = make([]byte, 0, normFlush+1)
	}
	m.Reset()
	return m, nil
}

// Reset discards everything written so far.
func (m *MinHasher) Reset() {
	for i := range m.bins {
		m.bins[i] = emptyBin
	}
	m.rolling, m.count, m.inToken = 0, 0, false
	if m.norm != nil {
		m.norm.Reset()
		m.pending = m.pending[:0]
	}
}

// Write tokenizes text by the default tokenizer; a token may span calls.
func (m *MinHasher) Write(text string) {
	normalize := m.norm != nil
	h, inToken, pending := m.token, m.inToken, m.pending
	for i := 0; i < len(text); {
		if wordByte[text[i]] == 0 {
			if inToken {
				m.add(uint64(h) % minhashModulus)
				inToken = false
			}
			i++
			continue
		}
		if !inToken {
			if normalize && m.count > 0 {
				pending = append(pending, ' ')
			}
			h = fnv32Offset
			inToken = true
		}
		if !normalize {
			for ; i < len(text) && wordByte[text[i]] != 0; i++ {
				h = (h * fnv32Prime) ^ uint32(wordByte[text[i]])
			}
			continue
		}
		for ; i < len(text); i++ {
			c := wordByte[text[i]]
			if c == 0 {
				break
			}
			h = (h * fnv32Prime) ^ uint32(c)
			if len(pending) >= normFlush {
				m.norm.Write(pending)
				pending = pending[:0]
			}
			pending = append(pending, c)
		}
	}
	m.token, m.inToken, m.pending = h, inToken, pending
}

// WriteToken adds one caller-made token as is; it ends any token in progress.
func (m *MinHasher) WriteToken(token string) {
	m.endToken()
	if m.norm != nil {
		if m.count > 0 {
			m.normByte(' ')
		}
		for i := 0; i < len(token); i++ {
			m.normByte(token[i])
		}
	}
	h := uint32(fnv32Offset)
	for i := 0; i < len(token); i++ {
		h = (h * fnv32Prime) ^ uint32(token[i])
	}
	m.add(uint64(h) % minhashModulus)
}

func (m *MinHasher) normByte(b byte) {
	if len(m.pending) >= normFlush {
		m.norm.Write(m.pending)
		m.pending = m.pending[:0]
	}
	m.pending = append(m.pending, b)
}

func (m *MinHasher) endToken() {
	if m.inToken {
		m.add(uint64(m.token) % minhashModulus)
		m.inToken = false
	}
}

// add takes the next token hash t: the shingle of the last Shingle tokens enters
// its bin once the window is full.
func (m *MinHasher) add(t uint64) {
	slot := m.count % m.shingle
	if m.count >= m.shingle {
		m.rolling = (m.rolling + minhashModulus - m.window[slot]*m.lead%minhashModulus) % minhashModulus
	}
	m.rolling = (m.rolling*minhashBase + t) % minhashModulus
	m.window[slot] = t
	m.count++
	if m.count >= m.shingle {
		m.insert(m.rolling)
	}
}

func (m *MinHasher) insert(s uint64) {
	k := uint64(len(m.bins))
	b, v := s%k, uint32(s/k)
	if v < m.bins[b] {
		m.bins[b] = v
	}
}

// MinHashResult is the summary of everything written.
type MinHashResult struct {
	Sketch     []uint32 // nil when there are no tokens
	Tokens     int
	Normalized [32]byte // zero when the hasher keeps no normalized digest
}

// Sum answers the result of everything written so far and keeps the state.
func (m *MinHasher) Sum() MinHashResult {
	var out MinHashResult
	if m.norm != nil {
		m.norm.Write(m.pending)
		m.pending = m.pending[:0]
		m.norm.Sum(out.Normalized[:0])
	}
	c := *m
	c.bins = append([]uint32(nil), m.bins...)
	c.window = append([]uint64(nil), m.window...)
	c.endToken()
	out.Tokens = c.count
	if c.count == 0 {
		return out
	}
	if c.count < c.shingle {
		c.insert(c.rolling)
	}
	out.Sketch = densify(c.bins)
	return out
}

// densify fills every empty bin i with the first non-empty bin of the probe
// sequence ((i+1)*C mod M + stride*a) mod k, a = 1..k.
func densify(bins []uint32) []uint32 {
	k := uint64(len(bins))
	out := make([]uint32, len(bins))
	for i, v := range bins {
		if v == emptyBin {
			start := (uint64(i+1) * minhashProbe) % minhashModulus
			for a := uint64(1); a <= k; a++ {
				if found := bins[(start+minhashStride*a)%k]; found != emptyBin {
					v = found
					break
				}
			}
		}
		out[i] = v
	}
	return out
}

// MinHashBands answers the band keys of sketch: band b covers bins
// Rows*b .. Rows*b+Rows-1 and its key is the first 8 bytes of
// sha256(frame(Domain, decimal b, the band's bins as 32-bit big-endian words)),
// where frame prefixes each field with its decimal byte length and a colon.
func MinHashBands(sketch []uint32, c MinHashConfig) ([][8]byte, error) {
	if err := c.validateBands(len(sketch)); err != nil {
		return nil, err
	}
	words := strconv.Itoa(4 * c.Rows)
	buf := make([]byte, 0, len(c.Domain)+len(words)+4*c.Rows+32)
	buf = strconv.AppendInt(buf, int64(len(c.Domain)), 10)
	buf = append(buf, ':')
	buf = append(buf, c.Domain...)
	prefix := len(buf)

	keys := make([][8]byte, c.Bands)
	for b := range keys {
		buf = buf[:prefix]
		index := strconv.Itoa(b)
		buf = strconv.AppendInt(buf, int64(len(index)), 10)
		buf = append(buf, ':')
		buf = append(buf, index...)
		buf = append(buf, words...)
		buf = append(buf, ':')
		for _, v := range sketch[b*c.Rows : (b+1)*c.Rows] {
			buf = binary.BigEndian.AppendUint32(buf, v)
		}
		sum := sha256.Sum256(buf)
		copy(keys[b][:], sum[:8])
	}
	return keys, nil
}

// MinHashSimilarity answers the share of equal bins of two sketches, the
// Jaccard similarity of the shingle sets they were computed from.
func MinHashSimilarity(a, b []uint32) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, errors.New("sketches must be non-empty and of equal size")
	}
	equal := 0
	for i, v := range a {
		if v == b[i] {
			equal++
		}
	}
	return float64(equal) / float64(len(a)), nil
}

// EncodeMinHashSketch answers sketch as consecutive 32-bit big-endian words.
func EncodeMinHashSketch(sketch []uint32) []byte {
	out := make([]byte, 0, 4*len(sketch))
	for _, v := range sketch {
		out = binary.BigEndian.AppendUint32(out, v)
	}
	return out
}

// DecodeMinHashSketch answers the sketch EncodeMinHashSketch rendered as data.
func DecodeMinHashSketch(data []byte) ([]uint32, error) {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil, errors.New("sketch must be a non-empty sequence of 32-bit words")
	}
	out := make([]uint32, len(data)/4)
	for i := range out {
		out[i] = binary.BigEndian.Uint32(data[4*i:])
	}
	return out, nil
}
