// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func ParseIdentityPublicKey(encoded string) (ed25519.PublicKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, fmt.Errorf("decode internode public key: %w", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("internode public key must decode to %d bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(decoded), nil
}

func ResolveIdentityKey(encoded, path string) (ed25519.PrivateKey, error) {
	if encoded != "" && path != "" {
		return nil, fmt.Errorf("internode identity key and identity key file are mutually exclusive")
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read internode identity key: %w", err)
		}
		encoded = strings.TrimSpace(string(data))
	}
	if encoded == "" {
		return nil, fmt.Errorf("internode identity key is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, fmt.Errorf("decode internode identity key: %w", err)
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(decoded), nil
	case ed25519.PrivateKeySize:
		return append(ed25519.PrivateKey(nil), decoded...), nil
	default:
		return nil, fmt.Errorf("internode identity key must decode to %d-byte seed or %d-byte private key", ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}
