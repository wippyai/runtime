// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const keyEnv = "RELEASE_SIGNING_KEY"

func loadKey(hexKey string) (ed25519.PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil {
		return nil, fmt.Errorf("decode hex key: %w", err)
	}

	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("key is %d bytes, want %d (seed) or %d (full key)", len(raw), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
}

func signFile(priv ed25519.PrivateKey, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	sig := ed25519.Sign(priv, data)
	encoded := base64.StdEncoding.EncodeToString(sig)

	return os.WriteFile(path+".sig", []byte(encoded+"\n"), 0o644)
}

func writePublicKey(priv ed25519.PrivateKey, dir string) (string, error) {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return "", errors.New("derived public key is not ed25519")
	}

	out := filepath.Join(dir, "release_key.pub")
	encoded := base64.StdEncoding.EncodeToString(pub)

	return out, os.WriteFile(out, []byte(encoded+"\n"), 0o644)
}

func run(args []string, hexKey string) error {
	if len(args) == 0 {
		return errors.New("provide at least one file to sign")
	}

	priv, err := loadKey(hexKey)
	if err != nil {
		return err
	}

	for _, path := range args {
		if err := signFile(priv, path); err != nil {
			return fmt.Errorf("sign %s: %w", path, err)
		}

		fmt.Printf("signed %s -> %s.sig\n", path, path)
	}

	pubPath, err := writePublicKey(priv, filepath.Dir(args[0]))
	if err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	fmt.Printf("wrote public key -> %s\n", pubPath)

	return nil
}

func main() {
	if err := run(os.Args[1:], os.Getenv(keyEnv)); err != nil {
		fmt.Fprintln(os.Stderr, "signrelease:", err)
		os.Exit(1)
	}
}
