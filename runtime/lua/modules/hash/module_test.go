// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"testing"

	lua "github.com/wippyai/go-lua"
	xpbkdf2 "golang.org/x/crypto/pbkdf2"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("hash")
	if mod.Type() != lua.LTTable {
		t.Fatal("hash module not registered")
	}

	modTbl := mod.(*lua.LTable)
	funcs := []string{"md5", "sha1", "sha256", "sha512", "fnv32", "fnv64", "hmac_sha256", "hmac_sha512", "hmac_sha1", "hmac_md5", "pbkdf2"}
	for _, fn := range funcs {
		if modTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("hash").(*lua.LTable)
	mod2 := l2.GetGlobal("hash").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestMD5(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.md5("hello")
		if err ~= nil then
			error("unexpected error: " .. tostring(err))
		end
		if result ~= "5d41402abc4b2a76b9719d911017c592" then
			error("md5 mismatch: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("md5 test failed: %v", err)
	}
}

func TestMD5Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.md5("hello", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 16 then
			error("md5 raw should be 16 bytes")
		end
	`)
	if err != nil {
		t.Errorf("md5 raw test failed: %v", err)
	}
}

func TestSHA1(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha1("hello")
		if err ~= nil then
			error("unexpected error")
		end
		if result ~= "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d" then
			error("sha1 mismatch: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("sha1 test failed: %v", err)
	}
}

func TestSHA256(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha256("hello")
		if err ~= nil then
			error("unexpected error")
		end
		if result ~= "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" then
			error("sha256 mismatch: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("sha256 test failed: %v", err)
	}
}

func TestSHA512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha512("hello")
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 128 then
			error("sha512 should be 128 hex chars")
		end
	`)
	if err != nil {
		t.Errorf("sha512 test failed: %v", err)
	}
}

func TestFNV32(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.fnv32("hello")
		if err ~= nil then
			error("unexpected error")
		end
		if type(result) ~= "number" then
			error("fnv32 should return number")
		end
	`)
	if err != nil {
		t.Errorf("fnv32 test failed: %v", err)
	}
}

func TestFNV64(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.fnv64("hello")
		if err ~= nil then
			error("unexpected error")
		end
		if type(result) ~= "number" then
			error("fnv64 should return number")
		end
	`)
	if err != nil {
		t.Errorf("fnv64 test failed: %v", err)
	}
}

func TestHMACSHA256(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha256("hello", "secret")
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 64 then
			error("hmac_sha256 should be 64 hex chars")
		end
	`)
	if err != nil {
		t.Errorf("hmac_sha256 test failed: %v", err)
	}
}

func TestHMACSHA512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha512("hello", "secret")
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 128 then
			error("hmac_sha512 should be 128 hex chars")
		end
	`)
	if err != nil {
		t.Errorf("hmac_sha512 test failed: %v", err)
	}
}

func TestHMACSHA1(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha1("hello", "secret")
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 40 then
			error("hmac_sha1 should be 40 hex chars")
		end
	`)
	if err != nil {
		t.Errorf("hmac_sha1 test failed: %v", err)
	}
}

func TestHMACMD5(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_md5("hello", "secret")
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 32 then
			error("hmac_md5 should be 32 hex chars")
		end
	`)
	if err != nil {
		t.Errorf("hmac_md5 test failed: %v", err)
	}
}

func TestHMACRaw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha256("hello", "secret", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 32 then
			error("hmac_sha256 raw should be 32 bytes")
		end
	`)
	if err != nil {
		t.Errorf("hmac_sha256 raw test failed: %v", err)
	}
}

func TestPBKDF2(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local key, err = hash.pbkdf2("password", "salt", 1000, 32)
		if err ~= nil then
			error("unexpected error: " .. tostring(err))
		end
		if #key ~= 32 then
			error("expected 32 byte key")
		end

		local key2, err2 = hash.pbkdf2("password", "salt", 1000, 32)
		if err2 ~= nil then
			error("unexpected error: " .. tostring(err2))
		end
		if key ~= key2 then
			error("pbkdf2 should be deterministic")
		end

		local key3, err3 = hash.pbkdf2("other", "salt", 1000, 32)
		if err3 ~= nil then
			error("unexpected error: " .. tostring(err3))
		end
		if key == key3 then
			error("different password should produce different key")
		end
	`)
	if err != nil {
		t.Errorf("pbkdf2 test failed: %v", err)
	}
}

func TestPBKDF2KnownVectors(t *testing.T) {
	tests := []struct {
		name     string
		password string
		salt     string
		algo     string
		wantHex  string

		iterations int
		keyLength  int
	}{
		{
			name:       "sha256 iteration 1",
			password:   "password",
			salt:       "salt",
			iterations: 1,
			keyLength:  32,
			algo:       "sha256",
			wantHex:    "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b",
		},
		{
			name:       "sha256 iteration 2",
			password:   "password",
			salt:       "salt",
			iterations: 2,
			keyLength:  32,
			algo:       "sha256",
			wantHex:    "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			switch tc.algo {
			case "sha256":
				got = derivePBKDF2([]byte(tc.password), []byte(tc.salt), tc.iterations, tc.keyLength, sha256.New)
			case "sha512":
				got = derivePBKDF2([]byte(tc.password), []byte(tc.salt), tc.iterations, tc.keyLength, sha512.New)
			default:
				t.Fatalf("unsupported test algo %q", tc.algo)
			}

			gotHex := hex.EncodeToString(got)
			if gotHex != tc.wantHex {
				t.Fatalf("pbkdf2 mismatch\nwant %s\n got %s", tc.wantHex, gotHex)
			}
		})
	}
}

func TestPBKDF2ParityWithReference(t *testing.T) {
	tests := []struct {
		name string
		algo string

		iterations int
		keyLength  int
	}{
		{name: "sha256", iterations: 4096, keyLength: 32, algo: "sha256"},
		{name: "sha512", iterations: 4096, keyLength: 64, algo: "sha512"},
		{name: "sha512 partial block", iterations: 4096, keyLength: 50, algo: "sha512"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got, want []byte
			switch tc.algo {
			case "sha256":
				got = derivePBKDF2([]byte("password"), []byte("salt"), tc.iterations, tc.keyLength, sha256.New)
				want = xpbkdf2.Key([]byte("password"), []byte("salt"), tc.iterations, tc.keyLength, sha256.New)
			case "sha512":
				got = derivePBKDF2([]byte("password"), []byte("salt"), tc.iterations, tc.keyLength, sha512.New)
				want = xpbkdf2.Key([]byte("password"), []byte("salt"), tc.iterations, tc.keyLength, sha512.New)
			}
			if string(got) != string(want) {
				t.Fatalf("pbkdf2 reference mismatch")
			}
		})
	}
}

func TestPBKDF2AllocationsDoNotScaleWithIterations(t *testing.T) {
	password := []byte("password")
	salt := []byte("salt")
	allocsOne := testing.AllocsPerRun(10, func() {
		_ = derivePBKDF2(password, salt, 1, 64, sha512.New)
	})
	allocsMany := testing.AllocsPerRun(10, func() {
		_ = derivePBKDF2(password, salt, 4096, 64, sha512.New)
	})

	if allocsMany > allocsOne+2 {
		t.Fatalf("pbkdf2 allocations scale with iterations: one=%0.2f many=%0.2f", allocsOne, allocsMany)
	}
}

func TestInvalidInputMD5(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.md5(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
		if err:retryable() ~= false then
			error("expected not retryable")
		end
	`)
	if err != nil {
		t.Errorf("invalid input test failed: %v", err)
	}
}

func TestInvalidInputHMAC(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha256(123, "secret")
		if result ~= nil then
			error("expected nil result for non-string data")
		end
		if err == nil then
			error("expected error for non-string data")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid data test failed: %v", err)
	}

	err = l.DoString(`
		local result, err = hash.hmac_sha256("hello", 123)
		if result ~= nil then
			error("expected nil result for non-string secret")
		end
		if err == nil then
			error("expected error for non-string secret")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid secret test failed: %v", err)
	}
}

func TestInvalidInputSHA1(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha1(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid input test failed: %v", err)
	}
}

func TestInvalidInputSHA256(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha256(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid input test failed: %v", err)
	}
}

func TestInvalidInputSHA512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha512(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid input test failed: %v", err)
	}
}

func TestInvalidInputFNV32(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.fnv32(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid input test failed: %v", err)
	}
}

func TestInvalidInputFNV64(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.fnv64(123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid input test failed: %v", err)
	}
}

func TestInvalidInputHMACSHA512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha512(123, "secret")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid data test failed: %v", err)
	}

	err = l.DoString(`
		local result, err = hash.hmac_sha512("hello", 123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
	`)
	if err != nil {
		t.Errorf("invalid secret test failed: %v", err)
	}
}

func TestInvalidInputHMACSHA1(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha1(123, "secret")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid data test failed: %v", err)
	}

	err = l.DoString(`
		local result, err = hash.hmac_sha1("hello", 123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
	`)
	if err != nil {
		t.Errorf("invalid secret test failed: %v", err)
	}
}

func TestInvalidInputHMACMD5(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_md5(123, "secret")
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end
	`)
	if err != nil {
		t.Errorf("invalid data test failed: %v", err)
	}

	err = l.DoString(`
		local result, err = hash.hmac_md5("hello", 123)
		if result ~= nil then
			error("expected nil result")
		end
		if err == nil then
			error("expected error")
		end
	`)
	if err != nil {
		t.Errorf("invalid secret test failed: %v", err)
	}
}

func TestSHA1Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha1("hello", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 20 then
			error("sha1 raw should be 20 bytes")
		end
	`)
	if err != nil {
		t.Errorf("sha1 raw test failed: %v", err)
	}
}

func TestSHA256Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha256("hello", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 32 then
			error("sha256 raw should be 32 bytes")
		end
	`)
	if err != nil {
		t.Errorf("sha256 raw test failed: %v", err)
	}
}

func TestSHA512Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.sha512("hello", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 64 then
			error("sha512 raw should be 64 bytes")
		end
	`)
	if err != nil {
		t.Errorf("sha512 raw test failed: %v", err)
	}
}

func TestHMACSHA512Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha512("hello", "secret", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 64 then
			error("hmac_sha512 raw should be 64 bytes")
		end
	`)
	if err != nil {
		t.Errorf("hmac_sha512 raw test failed: %v", err)
	}
}

func TestHMACSHA1Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_sha1("hello", "secret", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 20 then
			error("hmac_sha1 raw should be 20 bytes")
		end
	`)
	if err != nil {
		t.Errorf("hmac_sha1 raw test failed: %v", err)
	}
}

func TestHMACMD5Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = hash.hmac_md5("hello", "secret", true)
		if err ~= nil then
			error("unexpected error")
		end
		if #result ~= 16 then
			error("hmac_md5 raw should be 16 bytes")
		end
	`)
	if err != nil {
		t.Errorf("hmac_md5 raw test failed: %v", err)
	}
}

func TestDeterminism(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local h1 = hash.sha256("test")
		local h2 = hash.sha256("test")
		if h1 ~= h2 then
			error("hash should be deterministic")
		end

		local hm1 = hash.hmac_sha256("test", "key")
		local hm2 = hash.hmac_sha256("test", "key")
		if hm1 ~= hm2 then
			error("hmac should be deterministic")
		end
	`)
	if err != nil {
		t.Errorf("determinism test failed: %v", err)
	}
}
