package random

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	mathrand "math/rand"
	"sync"
	"time"
)

const (
	// SecureNamespace is the WASI random/random namespace.
	SecureNamespace = "wasi:random/random@0.2.0"
	// InsecureNamespace is the WASI random/insecure namespace.
	InsecureNamespace = "wasi:random/insecure@0.2.0"
	// InsecureSeedNamespace is the WASI random/insecure-seed namespace.
	InsecureSeedNamespace = "wasi:random/insecure-seed@0.2.0"
)

// MaxRandomBytes limits single-call allocation to prevent DoS (1MB).
const MaxRandomBytes = 1 << 20

var (
	insecureRand   = mathrand.New(mathrand.NewSource(time.Now().UnixNano())) //nolint:gosec // intentionally insecure per WASI spec
	insecureRandMu sync.Mutex
)

// SecureRandomHost provides cryptographically secure random numbers via crypto/rand.
type SecureRandomHost struct{}

// NewSecureRandomHost creates a secure random host.
func NewSecureRandomHost() *SecureRandomHost {
	return &SecureRandomHost{}
}

// Namespace implements wasm-runtime Host.
func (h *SecureRandomHost) Namespace() string {
	return SecureNamespace
}

// Register returns explicit WIT function mappings.
func (h *SecureRandomHost) Register() map[string]any {
	return map[string]any{
		"get-random-bytes": h.GetRandomBytes,
		"get-random-u64":   h.GetRandomU64,
	}
}

// GetRandomBytes returns cryptographically secure random bytes, capped at MaxRandomBytes.
func (h *SecureRandomHost) GetRandomBytes(_ context.Context, len uint64) []byte {
	if len > MaxRandomBytes {
		len = MaxRandomBytes
	}
	buf := make([]byte, len)
	if _, err := rand.Read(buf); err != nil {
		return nil
	}
	return buf
}

// GetRandomU64 returns a cryptographically secure random uint64.
func (h *SecureRandomHost) GetRandomU64(_ context.Context) uint64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0
	}
	return binary.LittleEndian.Uint64(buf[:])
}

// InsecureRandomHost provides fast non-cryptographic random numbers via math/rand.
type InsecureRandomHost struct{}

// NewInsecureRandomHost creates an insecure random host.
func NewInsecureRandomHost() *InsecureRandomHost {
	return &InsecureRandomHost{}
}

// Namespace implements wasm-runtime Host.
func (h *InsecureRandomHost) Namespace() string {
	return InsecureNamespace
}

// Register returns explicit WIT function mappings.
func (h *InsecureRandomHost) Register() map[string]any {
	return map[string]any{
		"get-insecure-random-bytes": h.GetInsecureRandomBytes,
		"get-insecure-random-u64":   h.GetInsecureRandomU64,
	}
}

// GetInsecureRandomBytes returns non-cryptographic random bytes.
func (h *InsecureRandomHost) GetInsecureRandomBytes(_ context.Context, len uint64) []byte {
	if len > MaxRandomBytes {
		len = MaxRandomBytes
	}
	buf := make([]byte, len)
	insecureRandMu.Lock()
	_, _ = insecureRand.Read(buf)
	insecureRandMu.Unlock()
	return buf
}

// GetInsecureRandomU64 returns a non-cryptographic random uint64.
func (h *InsecureRandomHost) GetInsecureRandomU64(_ context.Context) uint64 {
	insecureRandMu.Lock()
	result := insecureRand.Uint64()
	insecureRandMu.Unlock()
	return result
}

// InsecureSeedHost provides a random seed for hash map initialization.
type InsecureSeedHost struct{}

// NewInsecureSeedHost creates an insecure seed host.
func NewInsecureSeedHost() *InsecureSeedHost {
	return &InsecureSeedHost{}
}

// Namespace implements wasm-runtime Host.
func (h *InsecureSeedHost) Namespace() string {
	return InsecureSeedNamespace
}

// Register returns explicit WIT function mappings.
func (h *InsecureSeedHost) Register() map[string]any {
	return map[string]any{
		"insecure-seed": h.InsecureSeed,
	}
}

// InsecureSeed returns a pair of uint64 values derived from current time.
func (h *InsecureSeedHost) InsecureSeed(_ context.Context) (uint64, uint64) {
	now := time.Now().UnixNano()
	return uint64(now), uint64(now >> 32)
}
