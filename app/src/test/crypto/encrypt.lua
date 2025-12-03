-- Test: crypto.encrypt and crypto.decrypt functions
local assert = require("assert2")

local function main()
    local crypto = require("crypto")

    -- AES-256 encrypt/decrypt round trip
    local key32 = string.rep("a", 32)
    local plaintext = "Hello, World!"

    local ciphertext, err = crypto.encrypt.aes(plaintext, key32)
    assert.is_nil(err, "aes encrypt should not error")
    assert.not_nil(ciphertext, "aes encrypt returns ciphertext")
    assert.neq(ciphertext, plaintext, "ciphertext differs from plaintext")

    local decrypted, err2 = crypto.decrypt.aes(ciphertext, key32)
    assert.is_nil(err2, "aes decrypt should not error")
    assert.eq(decrypted, plaintext, "aes round-trip preserves data")

    -- AES with AAD
    local aad = "additional data"
    local ct_aad, err3 = crypto.encrypt.aes(plaintext, key32, aad)
    assert.is_nil(err3, "aes with aad encrypt should not error")

    local dec_aad, err4 = crypto.decrypt.aes(ct_aad, key32, aad)
    assert.is_nil(err4, "aes with aad decrypt should not error")
    assert.eq(dec_aad, plaintext, "aes with aad round-trip")

    -- Wrong AAD should fail
    local _, err5 = crypto.decrypt.aes(ct_aad, key32, "wrong aad")
    assert.not_nil(err5, "wrong aad should fail decryption")
    assert.eq(err5:kind(), errors.INTERNAL, "wrong aad error kind")
    assert.eq(err5:retryable(), false, "wrong aad not retryable")

    -- AES-128 key
    local key16 = string.rep("b", 16)
    local ct16, err6 = crypto.encrypt.aes("test", key16)
    assert.is_nil(err6, "aes-128 should work")
    local dec16, _ = crypto.decrypt.aes(ct16, key16)
    assert.eq(dec16, "test", "aes-128 round-trip")

    -- AES-192 key
    local key24 = string.rep("c", 24)
    local ct24, err7 = crypto.encrypt.aes("test", key24)
    assert.is_nil(err7, "aes-192 should work")
    local dec24, _ = crypto.decrypt.aes(ct24, key24)
    assert.eq(dec24, "test", "aes-192 round-trip")

    -- Invalid key length
    local _, err8 = crypto.encrypt.aes("test", "short")
    assert.not_nil(err8, "invalid key length should error")
    assert.eq(err8:kind(), errors.INVALID, "invalid key error kind")
    assert.eq(err8:retryable(), false, "invalid key not retryable")

    -- ChaCha20 encrypt/decrypt
    local cc_ct, err9 = crypto.encrypt.chacha20(plaintext, key32)
    assert.is_nil(err9, "chacha20 encrypt should not error")

    local cc_dec, err10 = crypto.decrypt.chacha20(cc_ct, key32)
    assert.is_nil(err10, "chacha20 decrypt should not error")
    assert.eq(cc_dec, plaintext, "chacha20 round-trip")

    -- ChaCha20 with AAD
    local cc_aad, err11 = crypto.encrypt.chacha20("test", key32, "aad")
    assert.is_nil(err11, "chacha20 with aad encrypt should not error")
    local cc_aad_dec, err12 = crypto.decrypt.chacha20(cc_aad, key32, "aad")
    assert.is_nil(err12, "chacha20 with aad decrypt should not error")
    assert.eq(cc_aad_dec, "test", "chacha20 with aad round-trip")

    -- ChaCha20 invalid key length
    local _, err13 = crypto.encrypt.chacha20("test", "short")
    assert.not_nil(err13, "chacha20 invalid key should error")
    assert.eq(err13:kind(), errors.INVALID, "chacha20 invalid key error kind")

    -- Wrong key should fail
    local wrong_key = string.rep("x", 32)
    local _, err14 = crypto.decrypt.aes(ciphertext, wrong_key)
    assert.not_nil(err14, "wrong key should fail")
    assert.eq(err14:kind(), errors.INTERNAL, "wrong key error kind")

    return true
end

return { main = main }
