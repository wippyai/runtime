// SPDX-License-Identifier: MPL-2.0

package toolchain

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
)

var (
	exeHashOnce sync.Once
	exeHashVal  string
	exeHashErr  error

	readBuildInfoFn    = debug.ReadBuildInfo
	executableSHA256Fn = ExecutableSHA256
)

// ModuleIdentity returns a stable hex identity for a Go module linked into the running binary.
// It reads from runtime/debug.ReadBuildInfo(). When the module is replaced by a local directory,
// or build info is unavailable, or the module is unknown, it returns the SHA-256 of the running executable.
func ModuleIdentity(path string) (string, error) {
	if info, ok := readBuildInfoFn(); ok && info != nil {
		if mod := findModule(info, path); mod != nil {
			if mod.Replace != nil {
				if mod.Replace.Version == "" && mod.Replace.Sum == "" {
					return executableSHA256Fn()
				}
				h := sha256.New()
				writeModuleInfo(h, mod.Path, mod.Version, mod.Sum)
				writeModuleInfo(h, mod.Replace.Path, mod.Replace.Version, mod.Replace.Sum)
				return hex.EncodeToString(h.Sum(nil)), nil
			}
			if mod.Version != "" || mod.Sum != "" {
				h := sha256.New()
				writeModuleInfo(h, mod.Path, mod.Version, mod.Sum)
				return hex.EncodeToString(h.Sum(nil)), nil
			}
		}
	}
	return executableSHA256Fn()
}

func findModule(info *debug.BuildInfo, path string) *debug.Module {
	if info.Main.Path == path {
		return &info.Main
	}
	for _, dep := range info.Deps {
		if dep != nil && dep.Path == path {
			return dep
		}
	}
	return nil
}

func writeModuleInfo(w io.Writer, path, version, sum string) {
	_, _ = w.Write([]byte(path))
	_, _ = w.Write([]byte{0})
	_, _ = w.Write([]byte(version))
	_, _ = w.Write([]byte{0})
	_, _ = w.Write([]byte(sum))
	_, _ = w.Write([]byte{0})
}

// ExecutableSHA256 computes the SHA-256 hex string of the running executable once per process.
func ExecutableSHA256() (string, error) {
	exeHashOnce.Do(func() {
		exeHashVal, exeHashErr = computeExecutableSHA256()
	})
	return exeHashVal, exeHashErr
}

func computeExecutableSHA256() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", NewExecutableError(err)
	}
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = realPath
	}
	f, err := os.Open(exePath)
	if err != nil {
		return "", NewExecutableError(err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", NewExecutableError(err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
