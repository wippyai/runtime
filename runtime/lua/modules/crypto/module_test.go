package crypto

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	mod := l.GetGlobal("crypto")
	if mod.Type() != lua.LTTable {
		t.Fatal("crypto module not registered")
	}

	tbl := mod.(*lua.LTable)
	if tbl.RawGetString("random").Type() != lua.LTTable {
		t.Error("random submodule not registered")
	}
	if tbl.RawGetString("encrypt").Type() != lua.LTTable {
		t.Error("encrypt submodule not registered")
	}
	if tbl.RawGetString("decrypt").Type() != lua.LTTable {
		t.Error("decrypt submodule not registered")
	}
	if tbl.RawGetString("pbkdf2").Type() != lua.LTFunction {
		t.Error("pbkdf2 function not registered")
	}
	if tbl.RawGetString("constant_time_compare").Type() != lua.LTFunction {
		t.Error("constant_time_compare function not registered")
	}
}

func TestRandomBytes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local bytes, err = crypto.random.bytes(16)
		if not bytes then error(err) end
		if #bytes ~= 16 then error("expected 16 bytes") end
	`)
	if err != nil {
		t.Errorf("random bytes test failed: %v", err)
	}
}

func TestRandomBytesInvalidLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.random.bytes(0)
		if err == nil then error("expected error for zero length") end

		_, err = crypto.random.bytes(-1)
		if err == nil then error("expected error for negative length") end
	`)
	if err != nil {
		t.Errorf("random bytes invalid length test failed: %v", err)
	}
}

func TestRandomString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local str, err = crypto.random.string(16)
		if not str then error(err) end
		if #str ~= 16 then error("expected 16 chars") end
	`)
	if err != nil {
		t.Errorf("random string test failed: %v", err)
	}
}

func TestRandomStringWithCharset(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local str, err = crypto.random.string(10, "abc")
		if not str then error(err) end
		for i = 1, #str do
			local c = str:sub(i, i)
			if c ~= "a" and c ~= "b" and c ~= "c" then
				error("invalid character in result")
			end
		end
	`)
	if err != nil {
		t.Errorf("random string with charset test failed: %v", err)
	}
}

func TestRandomStringEmptyCharset(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.random.string(10, "")
		if err == nil then error("expected error for empty charset") end
	`)
	if err != nil {
		t.Errorf("random string empty charset test failed: %v", err)
	}
}

func TestRandomUUID(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local id, err = crypto.random.uuid()
		if not id then error(err) end
		if #id ~= 36 then error("expected 36 char uuid") end
	`)
	if err != nil {
		t.Errorf("random uuid test failed: %v", err)
	}
}

func TestEncryptDecryptAES(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local plaintext = "Hello, World!"

		local ciphertext, err = crypto.encrypt.aes(plaintext, key)
		if not ciphertext then error(err) end

		local decrypted, err = crypto.decrypt.aes(ciphertext, key)
		if not decrypted then error(err) end

		if decrypted ~= plaintext then error("AES round trip failed") end
	`)
	if err != nil {
		t.Errorf("AES encrypt/decrypt test failed: %v", err)
	}
}

func TestEncryptDecryptAESWithAAD(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local plaintext = "Hello, World!"
		local aad = "additional data"

		local ciphertext, err = crypto.encrypt.aes(plaintext, key, aad)
		if not ciphertext then error(err) end

		local decrypted, err = crypto.decrypt.aes(ciphertext, key, aad)
		if not decrypted then error(err) end

		if decrypted ~= plaintext then error("AES with AAD round trip failed") end
	`)
	if err != nil {
		t.Errorf("AES with AAD test failed: %v", err)
	}
}

func TestAESInvalidKeyLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.encrypt.aes("test", "short")
		if err == nil then error("expected error for invalid key length") end
	`)
	if err != nil {
		t.Errorf("AES invalid key length test failed: %v", err)
	}
}

func TestAESDecryptInvalidData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local _, err = crypto.decrypt.aes("short", key)
		if err == nil then error("expected error for short ciphertext") end
	`)
	if err != nil {
		t.Errorf("AES decrypt invalid data test failed: %v", err)
	}
}

func TestEncryptDecryptChaCha20(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local plaintext = "Hello, World!"

		local ciphertext, err = crypto.encrypt.chacha20(plaintext, key)
		if not ciphertext then error(err) end

		local decrypted, err = crypto.decrypt.chacha20(ciphertext, key)
		if not decrypted then error(err) end

		if decrypted ~= plaintext then error("ChaCha20 round trip failed") end
	`)
	if err != nil {
		t.Errorf("ChaCha20 encrypt/decrypt test failed: %v", err)
	}
}

func TestChaCha20InvalidKeyLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.encrypt.chacha20("test", "short")
		if err == nil then error("expected error for invalid key length") end
	`)
	if err != nil {
		t.Errorf("ChaCha20 invalid key length test failed: %v", err)
	}
}

func TestChaCha20DecryptInvalidData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local _, err = crypto.decrypt.chacha20("short", key)
		if err == nil then error("expected error for short ciphertext") end
	`)
	if err != nil {
		t.Errorf("ChaCha20 decrypt invalid data test failed: %v", err)
	}
}

func TestPBKDF2(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key, err = crypto.pbkdf2("password", "salt", 1000, 32)
		if not key then error(err) end
		if #key ~= 32 then error("expected 32 byte key") end
	`)
	if err != nil {
		t.Errorf("pbkdf2 test failed: %v", err)
	}
}

func TestPBKDF2WithSHA512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key, err = crypto.pbkdf2("password", "salt", 1000, 64, "sha512")
		if not key then error(err) end
		if #key ~= 64 then error("expected 64 byte key") end
	`)
	if err != nil {
		t.Errorf("pbkdf2 with sha512 test failed: %v", err)
	}
}

func TestPBKDF2InvalidHash(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.pbkdf2("password", "salt", 1000, 32, "md5")
		if err == nil then error("expected error for unsupported hash") end
	`)
	if err != nil {
		t.Errorf("pbkdf2 invalid hash test failed: %v", err)
	}
}

func TestPBKDF2EmptyPassword(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.pbkdf2("", "salt", 1000, 32)
		if err == nil then error("expected error for empty password") end
	`)
	if err != nil {
		t.Errorf("pbkdf2 empty password test failed: %v", err)
	}
}

func TestPBKDF2EmptySalt(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.pbkdf2("password", "", 1000, 32)
		if err == nil then error("expected error for empty salt") end
	`)
	if err != nil {
		t.Errorf("pbkdf2 empty salt test failed: %v", err)
	}
}

func TestPBKDF2InvalidIterations(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.pbkdf2("password", "salt", 0, 32)
		if err == nil then error("expected error for zero iterations") end
	`)
	if err != nil {
		t.Errorf("pbkdf2 invalid iterations test failed: %v", err)
	}
}

func TestPBKDF2InvalidKeyLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.pbkdf2("password", "salt", 1000, 0)
		if err == nil then error("expected error for zero key length") end
	`)
	if err != nil {
		t.Errorf("pbkdf2 invalid key length test failed: %v", err)
	}
}

func TestConstantTimeCompare(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result = crypto.constant_time_compare("hello", "hello")
		if not result then error("expected true for equal strings") end

		result = crypto.constant_time_compare("hello", "world")
		if result then error("expected false for different strings") end
	`)
	if err != nil {
		t.Errorf("constant time compare test failed: %v", err)
	}
}

func TestAES128Key(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 16)
		local plaintext = "Hello"
		local ciphertext, err = crypto.encrypt.aes(plaintext, key)
		if not ciphertext then error(err) end
		local decrypted, err = crypto.decrypt.aes(ciphertext, key)
		if decrypted ~= plaintext then error("AES-128 round trip failed") end
	`)
	if err != nil {
		t.Errorf("AES-128 test failed: %v", err)
	}
}

func TestAES192Key(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 24)
		local plaintext = "Hello"
		local ciphertext, err = crypto.encrypt.aes(plaintext, key)
		if not ciphertext then error(err) end
		local decrypted, err = crypto.decrypt.aes(ciphertext, key)
		if decrypted ~= plaintext then error("AES-192 round trip failed") end
	`)
	if err != nil {
		t.Errorf("AES-192 test failed: %v", err)
	}
}

func TestAESDecryptWrongKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local wrongKey = string.rep("b", 32)
		local ciphertext, _ = crypto.encrypt.aes("test", key)
		local _, err = crypto.decrypt.aes(ciphertext, wrongKey)
		if err == nil then error("expected error for wrong key") end
	`)
	if err != nil {
		t.Errorf("AES wrong key test failed: %v", err)
	}
}

func TestChaCha20WithAAD(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local aad = "additional"
		local ciphertext, _ = crypto.encrypt.chacha20("test", key, aad)
		local decrypted, _ = crypto.decrypt.chacha20(ciphertext, key, aad)
		if decrypted ~= "test" then error("ChaCha20 with AAD failed") end
	`)
	if err != nil {
		t.Errorf("ChaCha20 with AAD test failed: %v", err)
	}
}

func TestChaCha20DecryptWrongKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local key = string.rep("a", 32)
		local wrongKey = string.rep("b", 32)
		local ciphertext, _ = crypto.encrypt.chacha20("test", key)
		local _, err = crypto.decrypt.chacha20(ciphertext, wrongKey)
		if err == nil then error("expected error for wrong key") end
	`)
	if err != nil {
		t.Errorf("ChaCha20 wrong key test failed: %v", err)
	}
}

func TestRandomStringInvalidLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.random.string(0)
		if err == nil then error("expected error for zero length") end
	`)
	if err != nil {
		t.Errorf("random string invalid length test failed: %v", err)
	}
}

func TestAESDecryptInvalidKeyLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.decrypt.aes("data", "short")
		if err == nil then error("expected error for invalid key length") end
	`)
	if err != nil {
		t.Errorf("AES decrypt invalid key test failed: %v", err)
	}
}

func TestChaCha20DecryptInvalidKeyLength(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.decrypt.chacha20("data", "short")
		if err == nil then error("expected error for invalid key length") end
	`)
	if err != nil {
		t.Errorf("ChaCha20 decrypt invalid key test failed: %v", err)
	}
}

func TestHMACSubmodule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	mod := l.GetGlobal("crypto").(*lua.LTable)
	hmacMod := mod.RawGetString("hmac")
	if hmacMod.Type() != lua.LTTable {
		t.Fatal("hmac submodule not registered")
	}

	tbl := hmacMod.(*lua.LTable)
	if tbl.RawGetString("sha256").Type() != lua.LTFunction {
		t.Error("hmac.sha256 function not registered")
	}
	if tbl.RawGetString("sha512").Type() != lua.LTFunction {
		t.Error("hmac.sha512 function not registered")
	}
}

func TestHMACSha256(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local digest, err = crypto.hmac.sha256("secret", "data")
		if not digest then error(err) end
		if #digest ~= 64 then error("expected 64 char hex digest") end
	`)
	if err != nil {
		t.Errorf("HMAC SHA256 test failed: %v", err)
	}
}

func TestHMACSha512(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local digest, err = crypto.hmac.sha512("secret", "data")
		if not digest then error(err) end
		if #digest ~= 128 then error("expected 128 char hex digest") end
	`)
	if err != nil {
		t.Errorf("HMAC SHA512 test failed: %v", err)
	}
}

func TestHMACEmptyKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.hmac.sha256("", "data")
		if err == nil then error("expected error for empty key") end
	`)
	if err != nil {
		t.Errorf("HMAC empty key test failed: %v", err)
	}
}

func TestHMACEmptyData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local digest, err = crypto.hmac.sha256("secret", "")
		if not digest then error(err) end
	`)
	if err != nil {
		t.Errorf("HMAC empty data test failed: %v", err)
	}
}

func TestJWTSubmodule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	mod := l.GetGlobal("crypto").(*lua.LTable)
	jwtMod := mod.RawGetString("jwt")
	if jwtMod.Type() != lua.LTTable {
		t.Fatal("jwt submodule not registered")
	}

	tbl := jwtMod.(*lua.LTable)
	if tbl.RawGetString("encode").Type() != lua.LTFunction {
		t.Error("jwt.encode function not registered")
	}
	if tbl.RawGetString("verify").Type() != lua.LTFunction {
		t.Error("jwt.verify function not registered")
	}
}

func TestJWTEncodeVerify(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local payload = { sub = "user123", name = "Test User" }
		local token, err = crypto.jwt.encode(payload, "secret")
		if not token then error(err) end

		local decoded, err = crypto.jwt.verify(token, "secret")
		if not decoded then error(err) end
		if decoded.sub ~= "user123" then error("sub mismatch") end
		if decoded.name ~= "Test User" then error("name mismatch") end
	`)
	if err != nil {
		t.Errorf("JWT encode/verify test failed: %v", err)
	}
}

func TestJWTVerifyInvalidToken(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local _, err = crypto.jwt.verify("invalid.token.here", "secret")
		if err == nil then error("expected error for invalid token") end
	`)
	if err != nil {
		t.Errorf("JWT invalid token test failed: %v", err)
	}
}

func TestJWTVerifyWrongKey(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local payload = { sub = "user123" }
		local token, _ = crypto.jwt.encode(payload, "secret1")
		local _, err = crypto.jwt.verify(token, "secret2")
		if err == nil then error("expected error for wrong key") end
	`)
	if err != nil {
		t.Errorf("JWT wrong key test failed: %v", err)
	}
}

func TestJWTAlgorithms(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local payload = { sub = "test" }

		-- HS256 (default)
		local t1, e1 = crypto.jwt.encode(payload, "secret")
		if not t1 then error(e1) end
		local d1, e1 = crypto.jwt.verify(t1, "secret")
		if not d1 then error(e1) end

		-- HS384
		local t2, e2 = crypto.jwt.encode(payload, "secret", "HS384")
		if not t2 then error(e2) end
		local d2, e2 = crypto.jwt.verify(t2, "secret", "HS384")
		if not d2 then error(e2) end

		-- HS512
		local t3, e3 = crypto.jwt.encode(payload, "secret", "HS512")
		if not t3 then error(e3) end
		local d3, e3 = crypto.jwt.verify(t3, "secret", "HS512")
		if not d3 then error(e3) end
	`)
	if err != nil {
		t.Errorf("JWT algorithms test failed: %v", err)
	}
}

// Security hardening tests

func TestRandomBytesMaxSizeLimit(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	// Test that exceeding max size returns error
	err := l.DoString(`
		local _, err = crypto.random.bytes(1024 * 1024 + 1)
		if err == nil then error("expected error for exceeding max size") end
		if not string.find(tostring(err), "exceeds maximum") then
			error("expected 'exceeds maximum' in error message")
		end
	`)
	if err != nil {
		t.Errorf("max size limit test failed: %v", err)
	}

	// Test that max size exactly works
	err = l.DoString(`
		local bytes, err = crypto.random.bytes(1024 * 1024)
		if not bytes then error(err) end
		if #bytes ~= 1024 * 1024 then error("expected 1MB bytes") end
	`)
	if err != nil {
		t.Errorf("max size exact test failed: %v", err)
	}
}

func TestRandomStringMaxSizeLimit(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	// Test that exceeding max size returns error
	err := l.DoString(`
		local _, err = crypto.random.string(1024 * 1024 + 1)
		if err == nil then error("expected error for exceeding max size") end
		if not string.find(tostring(err), "exceeds maximum") then
			error("expected 'exceeds maximum' in error message")
		end
	`)
	if err != nil {
		t.Errorf("max size limit test failed: %v", err)
	}
}

func TestRandomBytesDoSPrevention(t *testing.T) {
	// Verify the constant is set correctly
	if maxRandomSize != 1024*1024 {
		t.Errorf("maxRandomSize should be 1MB, got %d", maxRandomSize)
	}
}
