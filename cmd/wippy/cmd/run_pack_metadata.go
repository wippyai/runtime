// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

const runtimeMetadataPrefix = "runtime."

// loadLockRootRuntimeDefaults reads published host configuration from the
// selected deployment root. Hub bootstrap and offline lock restart therefore
// share one configuration authority; dependency packs never configure the host.
func loadLockRootRuntimeDefaults(lockPath string, logger *zap.Logger) (boot.Config, error) {
	lockObj, err := lock.New(lockPath)
	if err != nil {
		return nil, fmt.Errorf("load deployment lock %s: %w", lockPath, err)
	}
	roots := lockObj.GetRootModules()
	if len(roots) == 0 {
		return nil, nil
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("deployment lock must select exactly one root module")
	}
	root := roots[0]
	for _, loadPath := range lockObj.GetModuleLoadPaths() {
		if loadPath.Module != root || !loadPath.Root {
			continue
		}
		info, statErr := os.Stat(loadPath.Path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil, fmt.Errorf("selected deployment root %s is not installed; run wippy install", root)
			}
			return nil, fmt.Errorf("inspect selected deployment root %s: %w", root, statErr)
		}
		if info.IsDir() {
			return nil, nil
		}
		if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(loadPath.Path), ".wapp") {
			return nil, fmt.Errorf("selected deployment root %s has unsupported artifact %s", root, loadPath.Path)
		}
		return loadPackRuntimeDefaults(loadPath.Path, logger)
	}
	return nil, fmt.Errorf("selected deployment root %s is not installed; run wippy install", root)
}

// loadPackRuntimeDefaults reads runtime defaults from a single pack metadata.
func loadPackRuntimeDefaults(packPath string, logger *zap.Logger) (boot.Config, error) {
	file, err := os.Open(packPath)
	if err != nil {
		return nil, fmt.Errorf("open pack metadata %s: %w", packPath, err)
	}
	defer file.Close()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("read pack metadata %s: %w", packPath, err)
	}

	metadata, err := reader.GetMetadata()
	if err != nil {
		return nil, fmt.Errorf("read pack metadata %s: %w", packPath, err)
	}

	return runtimeConfigFromPackMetadata(metadata, logger), nil
}

// loadPackRuntimeDefaultsFromFiles reads runtime defaults from the application
// pack only. Dependency packs contribute registry entries and resources, but
// never configure the host application.
func loadPackRuntimeDefaultsFromFiles(packFiles []string, logger *zap.Logger) (boot.Config, error) {
	mainPackIndex := lastWappPackIndex(packFiles)
	if mainPackIndex < 0 {
		return nil, nil //nolint:nilnil // no pack means no packed defaults
	}
	return loadPackRuntimeDefaults(packFiles[mainPackIndex], logger)
}

func lastWappPackIndex(packFiles []string) int {
	for idx := len(packFiles) - 1; idx >= 0; idx-- {
		if hasWappExtension(packFiles[idx]) {
			return idx
		}
	}
	return -1
}

// runtimeConfigFromPackMetadata extracts runtime.* metadata keys and builds a boot config.
// Example supported keys: runtime.lsp.enabled=true
func runtimeConfigFromPackMetadata(metadata wapp.Metadata, logger *zap.Logger) boot.Config {
	if len(metadata) == 0 {
		return nil
	}

	flatRuntime := make(map[string]any)
	for key, val := range metadata {
		switch {
		case key == "runtime":
			flattenRuntimeMetadata(flatRuntime, "", val, false)
		case strings.HasPrefix(key, runtimeMetadataPrefix):
			runtimeKey := strings.Trim(strings.TrimPrefix(key, runtimeMetadataPrefix), ".")
			if runtimeKey == "" {
				continue
			}
			flatRuntime[runtimeKey] = normalizeRuntimeMetadataValue(val)
		}
	}

	if len(flatRuntime) == 0 {
		return nil
	}

	sections := make(map[string]map[string]any)
	for key, val := range flatRuntime {
		section, subKey, ok := strings.Cut(key, ".")
		if !ok || section == "" || subKey == "" {
			if logger != nil {
				logger.Debug("ignoring pack runtime metadata without section key", zap.String("key", key))
			}
			continue
		}

		if sections[section] == nil {
			sections[section] = make(map[string]any)
		}
		sections[section][subKey] = val
	}

	if len(sections) == 0 {
		return nil
	}

	opts := make([]boot.ConfigOption, 0, len(sections))
	for section, values := range sections {
		opts = append(opts, boot.WithSection(section, values))
	}

	return boot.NewConfig(opts...)
}

func flattenRuntimeMetadata(dst map[string]any, prefix string, value any, normalize bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == "" {
				continue
			}
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flattenRuntimeMetadata(dst, next, nested, normalize)
		}
	case map[any]any:
		for key, nested := range typed {
			strKey, ok := key.(string)
			if !ok || strKey == "" {
				continue
			}
			next := strKey
			if prefix != "" {
				next = prefix + "." + strKey
			}
			flattenRuntimeMetadata(dst, next, nested, normalize)
		}
	default:
		if prefix != "" {
			if normalize {
				value = normalizeRuntimeMetadataValue(value)
			}
			dst[prefix] = value
		}
	}
}

func normalizeRuntimeMetadataValue(value any) any {
	switch typed := value.(type) {
	case string:
		s := strings.TrimSpace(typed)
		switch strings.ToLower(s) {
		case "true":
			return true
		case "false":
			return false
		}

		if i, err := strconv.Atoi(s); err == nil {
			return i
		}
		return s
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		maxInt := int64(^uint(0) >> 1)
		minInt := -maxInt - 1
		if typed > maxInt || typed < minInt {
			return value
		}
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		maxInt := uint64(^uint(0) >> 1)
		if typed > maxInt {
			return value
		}
		return int(typed)
	default:
		return value
	}
}
