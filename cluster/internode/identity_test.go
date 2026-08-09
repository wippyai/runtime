// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveIdentityKey(t *testing.T) {
	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	encoded := base64.StdEncoding.EncodeToString(seed)
	expected := ed25519.NewKeyFromSeed(seed)

	fromValue, err := ResolveIdentityKey(encoded, "")
	require.NoError(t, err)
	require.Equal(t, expected, fromValue)
	publicKey := expected.Public().(ed25519.PublicKey)
	parsedPublicKey, err := ParseIdentityPublicKey(base64.StdEncoding.EncodeToString(publicKey))
	require.NoError(t, err)
	require.Equal(t, publicKey, parsedPublicKey)

	path := filepath.Join(t.TempDir(), "identity.key")
	require.NoError(t, os.WriteFile(path, []byte(encoded+"\n"), 0o600))
	fromFile, err := ResolveIdentityKey("", path)
	require.NoError(t, err)
	require.Equal(t, expected, fromFile)
}

func TestResolveIdentityKeyRejectsInvalidConfiguration(t *testing.T) {
	seed := base64.StdEncoding.EncodeToString(make([]byte, ed25519.SeedSize))
	_, err := ResolveIdentityKey("", "")
	require.Error(t, err)
	_, err = ResolveIdentityKey(seed, "identity.key")
	require.Error(t, err)
	_, err = ResolveIdentityKey(base64.StdEncoding.EncodeToString([]byte("short")), "")
	require.Error(t, err)
	_, err = ParseIdentityPublicKey(base64.StdEncoding.EncodeToString([]byte("short")))
	require.Error(t, err)
}
