// SPDX-License-Identifier: MPL-2.0

// Package nodepackage implements the build-time node-package artifact format.
package nodepackage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/wippyai/runtime/boot/deps/artifact"
)

const (
	FormatName          = "node-package"
	maxPackageJSONBytes = 1 << 20
	maxPackageNameBytes = 214
)

type Format struct{}

func New() *Format {
	return &Format{}
}

func (*Format) Name() string {
	return FormatName
}

func (*Format) Root() string {
	return "npm"
}

type manifest struct {
	Scripts map[string]json.RawMessage `json:"scripts"`
	Name    json.RawMessage            `json:"name"`
	Version json.RawMessage            `json:"version"`
}

func (*Format) Inspect(_ context.Context, input artifact.InspectInput) (artifact.Descriptor, error) {
	if input.Filesystem == nil {
		return artifact.Descriptor{}, errors.New("filesystem is nil")
	}
	data, err := readBoundedFile(input.Filesystem, "package.json", maxPackageJSONBytes)
	if err != nil {
		return artifact.Descriptor{}, err
	}

	var packageManifest manifest
	if err := json.Unmarshal(data, &packageManifest); err != nil {
		return artifact.Descriptor{}, fmt.Errorf("decode package.json: %w", err)
	}
	name, err := requiredString(packageManifest.Name, "name")
	if err != nil {
		return artifact.Descriptor{}, err
	}
	version, err := requiredString(packageManifest.Version, "version")
	if err != nil {
		return artifact.Descriptor{}, err
	}
	if err := validatePackageName(name); err != nil {
		return artifact.Descriptor{}, err
	}
	packageVersion, err := semver.NewVersion(version)
	if err != nil {
		return artifact.Descriptor{}, fmt.Errorf("package.json version %q is not semantic: %w", version, err)
	}
	if input.ModuleVersion != "" {
		moduleVersion, err := semver.NewVersion(input.ModuleVersion)
		if err != nil {
			return artifact.Descriptor{}, fmt.Errorf("module version %q is not semantic: %w", input.ModuleVersion, err)
		}
		if !packageVersion.Equal(moduleVersion) {
			return artifact.Descriptor{}, fmt.Errorf(
				"package version %s does not match module version %s", version, input.ModuleVersion)
		}
	}

	for _, script := range []string{"preinstall", "install", "postinstall", "prepare"} {
		if _, exists := packageManifest.Scripts[script]; exists {
			return artifact.Descriptor{}, fmt.Errorf("package.json lifecycle script %q is not allowed", script)
		}
	}

	return artifact.Descriptor{
		Identity:     name,
		Version:      version,
		RelativePath: path.Join("npm", name),
	}, nil
}

func readBoundedFile(filesystem fs.FS, name string, limit int64) ([]byte, error) {
	file, err := filesystem.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", name)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	return data, nil
}

func requiredString(raw json.RawMessage, field string) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("package.json %s is required", field)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("package.json %s must be a string", field)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("package.json %s must not be empty", field)
	}
	return value, nil
}

func validatePackageName(name string) error {
	if len(name) > maxPackageNameBytes {
		return fmt.Errorf("package name exceeds %d bytes", maxPackageNameBytes)
	}
	parts := []string{name}
	if strings.HasPrefix(name, "@") {
		parts = strings.Split(strings.TrimPrefix(name, "@"), "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid scoped package name %q", name)
		}
	} else if strings.Contains(name, "/") {
		return fmt.Errorf("invalid package name %q", name)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." ||
			strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
			return fmt.Errorf("invalid package name %q", name)
		}
		for _, char := range part {
			if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' ||
				char == '-' || char == '_' || char == '.' {
				continue
			}
			return fmt.Errorf("invalid package name %q", name)
		}
	}
	if len(parts) == 1 && (parts[0] == "node_modules" || parts[0] == "favicon.ico") {
		return fmt.Errorf("invalid package name %q", name)
	}
	return nil
}
