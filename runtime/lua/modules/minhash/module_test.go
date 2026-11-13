// SPDX-License-Identifier: MPL-2.0

package minhash

import (
	"encoding/hex"
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
)

func newState(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func TestLoad(t *testing.T) {
	l := newState(t)
	mod := l.GetGlobal("minhash").(*lua.LTable)
	for _, fn := range []string{"sketch", "signature", "bands", "estimate"} {
		if mod.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestLuaSignatureMatchesGoldenVectors(t *testing.T) {
	v := loadVectors(t)
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			l := newState(t)
			l.SetGlobal("text", lua.LString(build(c.Pieces)))
			l.SetGlobal("domain", lua.LString(v.Domain))
			if err := l.DoString(`result, err = minhash.signature(text, { domain = domain })`); err != nil {
				t.Fatal(err)
			}
			if err := l.GetGlobal("err"); err != lua.LNil {
				t.Fatalf("signature error: %v", err)
			}
			result := l.GetGlobal("result").(*lua.LTable)
			if words, ok := result.RawGetString("words").(lua.LInteger); !ok || int(words) != c.Words {
				t.Fatalf("words = %v, want %d", result.RawGetString("words"), c.Words)
			}
			if got := result.RawGetString("normalized_sha256").String(); got != c.NormalizedSHA256 {
				t.Fatalf("normalized_sha256 = %s", got)
			}
			if c.Sketch == nil {
				if result.RawGetString("sketch") != lua.LNil || result.RawGetString("bands") != lua.LNil {
					t.Fatal("sketch and bands must be absent for a text without tokens")
				}
				return
			}
			sketch, err := Decode([]byte(result.RawGetString("sketch").String()))
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(sketch) != fmt.Sprint(c.Sketch) {
				t.Fatalf("sketch = %v", sketch)
			}
			bands := result.RawGetString("bands").(*lua.LTable)
			if bands.Len() != len(c.Bands) {
				t.Fatalf("bands = %d", bands.Len())
			}
			for i, want := range c.Bands {
				if got := bands.RawGetInt(i + 1).String(); got != want {
					t.Fatalf("band %d = %s, want %s", i, got, want)
				}
			}
		})
	}
}

func TestLuaSketchBandsAndEstimate(t *testing.T) {
	v := loadVectors(t)
	l := newState(t)
	l.SetGlobal("a", lua.LString(build(v.Near.A)))
	l.SetGlobal("b", lua.LString(build(v.Near.B)))
	l.SetGlobal("domain", lua.LString(v.Domain))
	l.SetGlobal("want", lua.LNumber(float64(v.Near.EqualBins)/128))
	err := l.DoString(`
		local sa, err = minhash.sketch(a)
		assert(err == nil and #sa == 512, "sketch is 128 big-endian 32-bit words")
		local sb = minhash.sketch(b)
		local e, err2 = minhash.estimate(sa, sb)
		assert(err2 == nil and e == want, "estimate " .. tostring(e))
		assert(minhash.estimate(sa, sa) == 1, "self estimate is 1")

		local sig = minhash.signature(a, { domain = domain })
		assert(sig.sketch == sa, "signature carries the same sketch")
		local keys = minhash.bands(sa, { domain = domain })
		for i = 1, 16 do assert(keys[i] == sig.bands[i], "band " .. i) end

		local default = minhash.bands(sa)
		assert(#default == 16 and default[1] ~= keys[1], "default domain differs")

		local wide = minhash.bands(sa, { bands = 32, rows = 4 })
		assert(#wide == 32 and #wide[1] == 16, "32 bands of 64-bit hex keys")

		local empty, err3 = minhash.sketch(" ,;")
		assert(err3 == nil and empty == "", "no tokens give an empty sketch")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLuaOptionsSelectParameters(t *testing.T) {
	l := newState(t)
	p := Params{Shingle: 3, K: 64, Segment: 7, Modulus: 1000003, Base: 31, Probe: 7, Stride: 5}
	_, want, err := Sketch("Alpha beta gamma delta epsilon zeta eta theta", p)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.DoString(`
		got = minhash.sketch("Alpha beta gamma delta epsilon zeta eta theta",
			{ shingle = 3, k = 64, segment = 7.0, modulus = 1000003, base = 31, probe = 7, stride = 5 })
	`); err != nil {
		t.Fatal(err)
	}
	if got := l.GetGlobal("got").String(); got != string(Encode(want)) {
		t.Fatalf("sketch = %s, want %s", hex.EncodeToString([]byte(got)), hex.EncodeToString(Encode(want)))
	}
}

func TestLuaInvalidArguments(t *testing.T) {
	cases := map[string]string{
		"text not a string":         `return minhash.sketch(1)`,
		"options not a table":       `return minhash.sketch("a", 1)`,
		"unknown option":            `return minhash.sketch("a", { width = 5 })`,
		"band option on sketch":     `return minhash.sketch("a", { bands = 16 })`,
		"fractional integer":        `return minhash.sketch("a", { k = 1.5 })`,
		"negative integer":          `return minhash.sketch("a", { probe = -1 })`,
		"string integer":            `return minhash.sketch("a", { k = "128" })`,
		"stride shares k":           `return minhash.sketch("a", { stride = 64 })`,
		"modulus too large":         `return minhash.sketch("a", { modulus = 4294967296 })`,
		"bands times rows not k":    `return minhash.signature("a", { k = 64 })`,
		"domain not a string":       `return minhash.signature("a", { domain = 1 })`,
		"bands of a short sketch":   `return minhash.bands("abc")`,
		"bands not matching sketch": `return minhash.bands(minhash.sketch("a b"), { bands = 3, rows = 3 })`,
		"estimate of unequal sizes": `return minhash.estimate(minhash.sketch("a b"), minhash.sketch("a b", { k = 64 }))`,
		"estimate of empty sketch":  `return minhash.estimate("", "")`,
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			l := newState(t)
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
}
