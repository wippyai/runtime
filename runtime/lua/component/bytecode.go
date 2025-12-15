package component

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	fsapi "github.com/wippyai/runtime/api/fs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	glua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/bytecode"
)

// LoadBytecode reads bytecode bytes from the specified filesystem and path.
func LoadBytecode(fsReg fsapi.Registry, fsID, path string) ([]byte, error) {
	fs, ok := fsReg.GetFS(fsID)
	if !ok {
		return nil, luaapi.NewFilesystemNotFoundError(fsID)
	}

	file, err := fs.Open(path)
	if err != nil {
		return nil, luaapi.NewOpenFileError(path, err)
	}
	defer func() { _ = file.Close() }()

	return io.ReadAll(file)
}

// VerifyHash checks that the data bytes match the expected hash.
// Expected format: "sha256:hexstring"
func VerifyHash(data []byte, expected string) error {
	parts := strings.SplitN(expected, ":", 2)
	if len(parts) != 2 {
		return luaapi.NewInvalidHashFormatError(expected)
	}

	algorithm := parts[0]
	expectedHash := parts[1]

	var actualHash string
	switch algorithm {
	case "sha256":
		h := sha256.Sum256(data)
		actualHash = hex.EncodeToString(h[:])
	default:
		return luaapi.NewUnsupportedHashAlgorithmError(algorithm)
	}

	if actualHash != expectedHash {
		return luaapi.NewHashMismatchError(expectedHash, actualHash)
	}

	return nil
}

// UndumpBytecode deserializes bytecode bytes to a FunctionProto.
func UndumpBytecode(data []byte) (*glua.FunctionProto, error) {
	proto, err := bytecode.Undump(data)
	if err != nil {
		return nil, runtimelua.NewUndumpBytecodeError(err)
	}
	return proto, nil
}

// LoadAndVerifyBytecode loads bytecode from filesystem and verifies hash.
// Returns the deserialized FunctionProto.
func LoadAndVerifyBytecode(fsReg fsapi.Registry, fsID, path, hash string) (*glua.FunctionProto, error) {
	data, err := LoadBytecode(fsReg, fsID, path)
	if err != nil {
		return nil, err
	}

	if err := VerifyHash(data, hash); err != nil {
		return nil, err
	}

	return UndumpBytecode(data)
}
