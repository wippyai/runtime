// SPDX-License-Identifier: MPL-2.0

// Package minhash computes one-permutation MinHash sketches of text with optimal
// densification, their LSH band keys and Jaccard estimates. The algorithm is
// specified bit for bit in spec.md.
package minhash

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// Defaults of the specification.
const (
	DefaultShingle = 5
	DefaultK       = 128
	DefaultSegment = 32768
	DefaultModulus = 2147483647
	DefaultBase    = 1000003
	DefaultProbe   = 2654435761
	DefaultStride  = 55
	DefaultBands   = 16
	DefaultRows    = 8
	DefaultDomain  = "minhash.lsh.v1"
)

// Parameter bounds keep every intermediate product below 2^64 and every bin value
// within 32 bits.
const (
	MaxShingle = 1024
	MaxK       = 65536
	MaxModulus = 1<<31 - 1
	MaxBase    = 1<<32 - 1
	MaxProbe   = 1<<32 - 1
	MaxStride  = 1<<32 - 1
)

const (
	fnvOffset = 0x811C9DC5
	fnvPrime  = 0x01000193
	emptyBin  = ^uint32(0)
)

// Params configures tokenization, shingling and the sketch.
type Params struct {
	Shingle int    // tokens per shingle
	K       int    // sketch bins
	Segment int    // bytes per tokenization segment
	Modulus uint64 // prime field of token and shingle hashes
	Base    uint64 // polynomial base of shingle hashes
	Probe   uint64 // multiplier of the densification probe start
	Stride  uint64 // densification probe step, coprime with K
}

// DefaultParams answers the parameters of the specification.
func DefaultParams() Params {
	return Params{
		Shingle: DefaultShingle,
		K:       DefaultK,
		Segment: DefaultSegment,
		Modulus: DefaultModulus,
		Base:    DefaultBase,
		Probe:   DefaultProbe,
		Stride:  DefaultStride,
	}
}

// Validate answers why p cannot be used, or nil.
func (p Params) Validate() error {
	switch {
	case p.Shingle < 1 || p.Shingle > MaxShingle:
		return fmt.Errorf("shingle must be in 1..%d", MaxShingle)
	case p.K < 1 || p.K > MaxK:
		return fmt.Errorf("k must be in 1..%d", MaxK)
	case p.Segment < 1:
		return errors.New("segment must be positive")
	case p.Modulus < 2 || p.Modulus > MaxModulus:
		return fmt.Errorf("modulus must be in 2..%d", uint64(MaxModulus))
	case p.Base < 1 || p.Base > MaxBase:
		return fmt.Errorf("base must be in 1..%d", uint64(MaxBase))
	case p.Probe > MaxProbe:
		return fmt.Errorf("probe must be in 0..%d", uint64(MaxProbe))
	case p.Stride < 1 || p.Stride > MaxStride:
		return fmt.Errorf("stride must be in 1..%d", uint64(MaxStride))
	case gcd(p.Stride, uint64(p.K)) != 1:
		return errors.New("stride must be coprime with k")
	}
	return nil
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

type scratch struct {
	hashes []uint32
	bins   []uint32
	norm   []byte
}

var scratchPool = sync.Pool{New: func() any { return new(scratch) }}

// Sketch answers the token count of text and its sketch, nil when text has no tokens.
func Sketch(text string, p Params) (int, []uint32, error) {
	if err := p.Validate(); err != nil {
		return 0, nil, err
	}
	s := scratchPool.Get().(*scratch)
	defer scratchPool.Put(s)
	s.tokenize(text, p, false)
	return len(s.hashes), s.sketch(p), nil
}

// Result is everything a text is compared by.
type Result struct {
	Sketch     []uint32 // nil when the text has no tokens
	Words      int      // token count
	Normalized [32]byte // sha256 of the tokens joined by one space
}

// Signature answers the result of text.
func Signature(text string, p Params) (Result, error) {
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	s := scratchPool.Get().(*scratch)
	defer scratchPool.Put(s)
	s.tokenize(text, p, true)
	return Result{Sketch: s.sketch(p), Words: len(s.hashes), Normalized: sha256.Sum256(s.norm)}, nil
}

// tokenize fills s.hashes with the token hashes of text and, when normalize is set,
// s.norm with the tokens joined by one space. A segment ends just after the last
// separator of a window of p.Segment bytes, or at the window end when it holds none.
func (s *scratch) tokenize(text string, p Params, normalize bool) {
	s.hashes = s.hashes[:0]
	s.norm = s.norm[:0]
	n := len(text)
	for pos := 0; pos < n; {
		end := n
		if n-pos > p.Segment {
			end = pos + p.Segment
			k := end - 1
			for k >= pos && wordByte[text[k]] != 0 {
				k--
			}
			if k >= pos {
				end = k + 1
			}
		}
		h := uint32(fnvOffset)
		inToken := false
		for i := pos; i < end; i++ {
			c := wordByte[text[i]]
			if c == 0 {
				if inToken {
					s.hashes = append(s.hashes, uint32(uint64(h)%p.Modulus))
					inToken = false
				}
				continue
			}
			if !inToken {
				if normalize && len(s.hashes) > 0 {
					s.norm = append(s.norm, ' ')
				}
				h = fnvOffset
				inToken = true
			}
			h = (h * fnvPrime) ^ uint32(c)
			if normalize {
				s.norm = append(s.norm, c)
			}
		}
		if inToken {
			s.hashes = append(s.hashes, uint32(uint64(h)%p.Modulus))
		}
		pos = end
	}
}

// sketch answers the densified one-permutation MinHash of s.hashes, nil when empty.
func (s *scratch) sketch(p Params) []uint32 {
	hashes := s.hashes
	if len(hashes) == 0 {
		return nil
	}
	if cap(s.bins) < p.K {
		s.bins = make([]uint32, p.K)
	}
	bins := s.bins[:p.K]
	for i := range bins {
		bins[i] = emptyBin
	}

	m, base, k := p.Modulus, p.Base, uint64(p.K)
	width := min(p.Shingle, len(hashes))
	lead := uint64(1)
	for range width - 1 {
		lead = lead * base % m
	}
	var h uint64
	for _, t := range hashes[:width-1] {
		h = (h*base + uint64(t)) % m
	}
	for start := 0; start+width <= len(hashes); start++ {
		if start > 0 {
			h = (h + m - uint64(hashes[start-1])*lead%m) % m
		}
		h = (h*base + uint64(hashes[start+width-1])) % m
		b, v := h%k, uint32(h/k)
		if v < bins[b] {
			bins[b] = v
		}
	}

	out := make([]uint32, p.K)
	for i, v := range bins {
		if v == emptyBin {
			start := (uint64(i+1) * p.Probe) % m
			for a := uint64(1); a <= k; a++ {
				if found := bins[(start+p.Stride*a)%k]; found != emptyBin {
					v = found
					break
				}
			}
		}
		out[i] = v
	}
	return out
}

// Estimate answers the share of equal bins of two sketches, the Jaccard similarity
// they estimate.
func Estimate(a, b []uint32) (float64, error) {
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

// LSH configures band keys over a sketch of Bands*Rows bins.
type LSH struct {
	Domain string
	Bands  int
	Rows   int
}

// DefaultLSH answers the banding of the specification.
func DefaultLSH() LSH {
	return LSH{Domain: DefaultDomain, Bands: DefaultBands, Rows: DefaultRows}
}

// Keys answers the band keys of sketch: band b covers bins Rows*b .. Rows*b+Rows-1
// and its key is the first 8 bytes of sha256(frame(Domain, decimal b, the band's
// bins as 32-bit big-endian words)), where frame prefixes each field with its
// decimal length and a colon.
func (l LSH) Keys(sketch []uint32) ([][8]byte, error) {
	if l.Bands < 1 || l.Rows < 1 {
		return nil, errors.New("bands and rows must be positive")
	}
	if l.Bands*l.Rows != len(sketch) {
		return nil, fmt.Errorf("bands*rows must equal the sketch size %d", len(sketch))
	}
	words := strconv.Itoa(4 * l.Rows)
	buf := make([]byte, 0, len(l.Domain)+len(words)+4*l.Rows+32)
	buf = strconv.AppendInt(buf, int64(len(l.Domain)), 10)
	buf = append(buf, ':')
	buf = append(buf, l.Domain...)
	prefix := len(buf)

	keys := make([][8]byte, l.Bands)
	for b := range keys {
		buf = buf[:prefix]
		index := strconv.Itoa(b)
		buf = strconv.AppendInt(buf, int64(len(index)), 10)
		buf = append(buf, ':')
		buf = append(buf, index...)
		buf = append(buf, words...)
		buf = append(buf, ':')
		for _, v := range sketch[b*l.Rows : (b+1)*l.Rows] {
			buf = binary.BigEndian.AppendUint32(buf, v)
		}
		sum := sha256.Sum256(buf)
		copy(keys[b][:], sum[:8])
	}
	return keys, nil
}

// Encode answers sketch as consecutive 32-bit big-endian words.
func Encode(sketch []uint32) []byte {
	out := make([]byte, 0, 4*len(sketch))
	for _, v := range sketch {
		out = binary.BigEndian.AppendUint32(out, v)
	}
	return out
}

// Decode answers the sketch Encode rendered as data.
func Decode(data []byte) ([]uint32, error) {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil, errors.New("sketch must be a non-empty sequence of 32-bit words")
	}
	out := make([]uint32, len(data)/4)
	for i := range out {
		out[i] = binary.BigEndian.Uint32(data[4*i:])
	}
	return out, nil
}
