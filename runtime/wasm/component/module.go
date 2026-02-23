// SPDX-License-Identifier: MPL-2.0

package component

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	fsapi "github.com/wippyai/runtime/api/fs"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
)

// LoadWASM reads wasm bytes from the specified filesystem and path.
func LoadWASM(fsReg fsapi.Registry, fsID, path string) ([]byte, error) {
	fs, ok := fsReg.GetFS(fsID)
	if !ok {
		return nil, runtimewasm.NewFilesystemNotFoundError(fsID)
	}

	file, err := fs.Open(path)
	if err != nil {
		return nil, runtimewasm.NewOpenFileError(path, err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, runtimewasm.NewFilesystemReadError(err)
	}

	return data, nil
}

// VerifyHash checks data against expected hash in "<algorithm>:<hex>" format.
func VerifyHash(data []byte, expected string) error {
	parts := strings.SplitN(expected, ":", 2)
	if len(parts) != 2 {
		return runtimewasm.NewInvalidHashFormatError(expected)
	}

	algorithm := parts[0]
	expectedHash := parts[1]

	var actualHash string
	switch algorithm {
	case "sha256":
		sum := sha256.Sum256(data)
		actualHash = hex.EncodeToString(sum[:])
	default:
		return runtimewasm.NewUnsupportedHashAlgorithmError(algorithm)
	}

	if actualHash != expectedHash {
		return runtimewasm.NewHashMismatchError(expectedHash, actualHash)
	}

	return nil
}

// LoadAndVerifyWASM loads wasm bytes from filesystem and verifies hash.
func LoadAndVerifyWASM(fsReg fsapi.Registry, fsID, path, hash string) ([]byte, error) {
	data, err := LoadWASM(fsReg, fsID, path)
	if err != nil {
		return nil, err
	}

	if err := VerifyHash(data, hash); err != nil {
		return nil, runtimewasm.NewHashVerificationError(err)
	}

	return data, nil
}
