// SPDX-License-Identifier: MPL-2.0

// Package app hosts native applications built on the Wippy runtime.
package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/api/semver"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
)

// Pack is an exact published artifact, including its Hub identity.
// Digest is the SHA-256 of Data, with a sha256: prefix.
type Pack struct {
	Module  string
	Version string
	Digest  string
	Data    []byte
}

// Bundle is the complete offline deployment shipped by an executable.
// Root selects the application; other packs are its locked dependencies.
type Bundle struct {
	Root  string
	Packs []Pack
}

func (bundle Bundle) validate() error {
	if err := lock.ValidateModuleName(bundle.Root); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, pack := range bundle.Packs {
		if err := lock.ValidateModuleName(pack.Module); err != nil {
			return err
		}
		if _, err := semver.ParseVersion(pack.Version); err != nil {
			return fmt.Errorf("pack %s version: %w", pack.Module, err)
		}
		if seen[pack.Module] {
			return fmt.Errorf("duplicate bundled module %s", pack.Module)
		}
		seen[pack.Module] = true
		digest := sha256.Sum256(pack.Data)
		if pack.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
			return fmt.Errorf("pack %s digest mismatch", pack.Module)
		}
		reader, err := wapp.NewReader(bytes.NewReader(pack.Data))
		if err != nil {
			return fmt.Errorf("pack %s: %w", pack.Module, err)
		}
		metadata, err := reader.GetMetadata()
		if err != nil {
			return fmt.Errorf("pack %s metadata: %w", pack.Module, err)
		}
		name, _ := graph.ParseName(pack.Module)
		if metadata["namespace"] != name.Organization+"."+name.Module || metadata["name"] != name.Module || metadata["version"] != pack.Version {
			return fmt.Errorf("pack %s metadata does not match bundled identity", pack.Module)
		}
	}
	if !seen[bundle.Root] {
		return fmt.Errorf("bundled application %s is missing", bundle.Root)
	}
	return nil
}

// Seed installs an initial Wippy deployment in directory and returns its
// lock path. An existing deployment must select the same application and is
// never replaced, including when its version differs from the embedded pack.
// Directory contains deployment files. Store application databases separately.
func (bundle Bundle) Seed(directory string) (string, error) {
	if err := bundle.validate(); err != nil {
		return "", err
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	lockPath := filepath.Join(directory, lock.DefaultFilename)
	if _, err := os.Stat(directory); err == nil {
		return bundle.existing(lockPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(parent, ".deployment-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	locked, err := lock.New(filepath.Join(staging, lock.DefaultFilename))
	if err != nil {
		return "", err
	}
	locked.SetDirectories(lock.Directories{Modules: ".wippy", Src: "src"})
	for _, pack := range bundle.Packs {
		name, _ := graph.ParseName(pack.Module)
		path := filepath.Join(staging, locked.GetVendorPath(), lock.WappPath(name, pack.Version))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if err := writeSynced(path, pack.Data); err != nil {
			return "", err
		}
		locked.SetModule(lock.Module{Name: pack.Module, Version: pack.Version, Hash: pack.Digest, Root: pack.Module == bundle.Root})
	}
	if err := locked.Write(); err != nil {
		return "", err
	}
	if err := os.Rename(staging, directory); err != nil {
		// A concurrent first launch may have installed the same application.
		if _, statErr := os.Stat(directory); statErr == nil {
			return bundle.existing(lockPath)
		}
		return "", err
	}
	return lockPath, nil
}

func (bundle Bundle) existing(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("existing deployment lock: %w", err)
	}
	locked, err := lock.New(path)
	if err != nil {
		return "", err
	}
	if err := lock.Validate(locked); err != nil {
		return "", err
	}
	roots := locked.GetRootModules()
	if len(roots) != 1 || roots[0] != bundle.Root {
		return "", fmt.Errorf("deployment does not select %s", bundle.Root)
	}
	return path, nil
}

func writeSynced(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
