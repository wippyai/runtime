package hash

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	mod := l.GetGlobal("hash")
	if mod.Type() != lua.LTTable {
		t.Fatal("hash module not registered")
	}

	tbl := mod.(*lua.LTable)
	funcs := []string{"md5", "sha1", "sha256", "sha512", "fnv32", "fnv64", "hmac_sha256", "hmac_sha512", "hmac_sha1", "hmac_md5"}
	for _, fn := range funcs {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestMD5(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result = hash.md5("hello")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.md5("hello", true)
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
	Bind(l)

	err := l.DoString(`
		local result = hash.sha1("hello")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.sha256("hello")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.sha512("hello")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.fnv32("hello")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.fnv64("hello")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_sha256("hello", "secret")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_sha512("hello", "secret")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_sha1("hello", "secret")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_md5("hello", "secret")
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_sha256("hello", "secret", true)
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
	Bind(l)

	err := l.DoString(`hash.md5(123)`)
	if err == nil {
		t.Error("expected error for non-string input")
	}
}

func TestInvalidInputHMAC(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.hmac_sha256(123, "secret")`)
	if err == nil {
		t.Error("expected error for non-string data")
	}

	err = l.DoString(`hash.hmac_sha256("hello", 123)`)
	if err == nil {
		t.Error("expected error for non-string secret")
	}
}

func TestInvalidInputSHA1(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.sha1(123)`)
	if err == nil {
		t.Error("expected error for non-string input")
	}
}

func TestInvalidInputSHA256(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.sha256(123)`)
	if err == nil {
		t.Error("expected error for non-string input")
	}
}

func TestInvalidInputSHA512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.sha512(123)`)
	if err == nil {
		t.Error("expected error for non-string input")
	}
}

func TestInvalidInputFNV32(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.fnv32(123)`)
	if err == nil {
		t.Error("expected error for non-string input")
	}
}

func TestInvalidInputFNV64(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.fnv64(123)`)
	if err == nil {
		t.Error("expected error for non-string input")
	}
}

func TestInvalidInputHMACSHA512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.hmac_sha512(123, "secret")`)
	if err == nil {
		t.Error("expected error for non-string data")
	}

	err = l.DoString(`hash.hmac_sha512("hello", 123)`)
	if err == nil {
		t.Error("expected error for non-string secret")
	}
}

func TestInvalidInputHMACSHA1(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.hmac_sha1(123, "secret")`)
	if err == nil {
		t.Error("expected error for non-string data")
	}

	err = l.DoString(`hash.hmac_sha1("hello", 123)`)
	if err == nil {
		t.Error("expected error for non-string secret")
	}
}

func TestInvalidInputHMACMD5(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`hash.hmac_md5(123, "secret")`)
	if err == nil {
		t.Error("expected error for non-string data")
	}

	err = l.DoString(`hash.hmac_md5("hello", 123)`)
	if err == nil {
		t.Error("expected error for non-string secret")
	}
}

func TestSHA1Raw(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result = hash.sha1("hello", true)
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
	Bind(l)

	err := l.DoString(`
		local result = hash.sha256("hello", true)
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
	Bind(l)

	err := l.DoString(`
		local result = hash.sha512("hello", true)
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_sha512("hello", "secret", true)
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_sha1("hello", "secret", true)
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
	Bind(l)

	err := l.DoString(`
		local result = hash.hmac_md5("hello", "secret", true)
		if #result ~= 16 then
			error("hmac_md5 raw should be 16 bytes")
		end
	`)
	if err != nil {
		t.Errorf("hmac_md5 raw test failed: %v", err)
	}
}
