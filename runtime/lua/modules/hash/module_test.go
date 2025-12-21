package hash

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
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
	funcs := []string{"md5", "sha1", "sha256", "sha512", "fnv32", "fnv64", "hmac_sha256", "hmac_sha512", "hmac_sha1", "hmac_md5"}
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
