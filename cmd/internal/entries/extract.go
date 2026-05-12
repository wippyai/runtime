// SPDX-License-Identifier: MPL-2.0

package entries

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wippyai/wapp"
	"gopkg.in/yaml.v3"
)

// ExtractWappToDir extracts a .wapp file into a source directory with _index.yaml files
// and source files. After extraction, the .wapp file is removed.
// projectRoot is the directory containing wippy.lock, used to compute relative paths
// for fs.directory entries reconstructed from embedded resources.
func ExtractWappToDir(wappPath, targetDir, projectRoot string) error {
	// Open and keep file open throughout extraction so resource FS handles remain valid
	file, err := os.Open(wappPath)
	if err != nil {
		return fmt.Errorf("open wapp file: %w", err)
	}
	defer file.Close()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return fmt.Errorf("create wapp reader: %w", err)
	}

	entries, err := reader.GetEntries()
	if err != nil {
		return fmt.Errorf("read entries: %w", err)
	}

	var resources []extractedResource
	for _, res := range reader.ListResources() {
		resFS, err := reader.GetFS(res.ID)
		if err != nil {
			return fmt.Errorf("get resource filesystem %s: %w", res.ID.String(), err)
		}
		resources = append(resources, extractedResource{
			id:   res.ID.String(),
			nsID: res.ID,
			fs:   resFS,
		})
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	// Reconstruct fs.embed entries as fs.directory with extracted resource files
	entries, resources, err = restoreEmbeddedResources(entries, resources, targetDir, projectRoot)
	if err != nil {
		return err
	}

	// Group entries by namespace
	grouped := make(map[string][]wapp.Entry)
	var namespaces []string
	for _, entry := range entries {
		ns := entry.ID.Namespace
		if _, seen := grouped[ns]; !seen {
			namespaces = append(namespaces, ns)
		}
		grouped[ns] = append(grouped[ns], entry)
	}

	// Determine output directories per namespace
	nsDirs := resolveNamespaceDirs(targetDir, namespaces)

	for _, ns := range namespaces {
		nsDir := nsDirs[ns]
		if err := os.MkdirAll(nsDir, 0755); err != nil {
			return fmt.Errorf("create namespace directory: %w", err)
		}
		if err := writeNamespaceIndex(nsDir, ns, grouped[ns]); err != nil {
			return fmt.Errorf("write index for namespace %s: %w", ns, err)
		}
	}

	// Extract remaining unclaimed tree resources
	for _, res := range resources {
		if err := extractResourceFS(targetDir, res.fs); err != nil {
			return fmt.Errorf("extract resource %s: %w", res.id, err)
		}
	}

	// Close file before removing
	file.Close()

	if err := os.Remove(wappPath); err != nil {
		return fmt.Errorf("remove wapp file: %w", err)
	}

	return nil
}

type extractedResource struct {
	fs   fs.ReadDirFS
	nsID wapp.ID
	id   string
}

// restoreEmbeddedResources converts fs.embed entries back to fs.directory by extracting
// their matching resource filesystems to named subdirectories. Returns the modified entries
// and any unclaimed resources.
func restoreEmbeddedResources(entries []wapp.Entry, resources []extractedResource, targetDir, _ string) ([]wapp.Entry, []extractedResource, error) {
	if len(resources) == 0 {
		return entries, resources, nil
	}

	// Build resource lookup by namespace:name
	resMap := make(map[string]int, len(resources))
	for i, res := range resources {
		resMap[res.id] = i
	}

	claimed := make(map[int]bool)
	result := make([]wapp.Entry, len(entries))

	for i, entry := range entries {
		if entry.Kind != "fs.embed" {
			result[i] = entry
			continue
		}

		entryKey := entry.ID.String()
		resIdx, found := resMap[entryKey]
		if !found {
			result[i] = entry
			continue
		}

		// Extract resource files to a subdirectory named after the entry
		resDir := filepath.Join(targetDir, entry.ID.Name)
		if err := os.MkdirAll(resDir, 0755); err != nil {
			return nil, nil, fmt.Errorf("create resource directory %s: %w", entry.ID.Name, err)
		}
		if err := extractResourceFS(resDir, resources[resIdx].fs); err != nil {
			return nil, nil, fmt.Errorf("extract embedded resource %s: %w", entryKey, err)
		}

		// Convert fs.embed back to fs.directory.
		// Path is module-relative; the runtime joins it with the module SourceRoot.
		result[i] = wapp.Entry{
			ID:   entry.ID,
			Kind: "fs.directory",
			Meta: entry.Meta,
			Data: map[string]any{
				"directory": entry.ID.Name,
				"base":      "module",
			},
		}

		claimed[resIdx] = true
	}

	// Collect unclaimed resources
	var remaining []extractedResource
	for i, res := range resources {
		if !claimed[i] {
			remaining = append(remaining, res)
		}
	}

	return result, remaining, nil
}

// resolveNamespaceDirs maps each namespace to the directory where its _index.yaml goes.
// Single namespace: root targetDir.
// Multiple namespaces: subdirectories derived by stripping the common prefix.
func resolveNamespaceDirs(targetDir string, namespaces []string) map[string]string {
	dirs := make(map[string]string, len(namespaces))
	if len(namespaces) <= 1 {
		for _, ns := range namespaces {
			dirs[ns] = targetDir
		}
		return dirs
	}

	prefix := commonDotPrefix(namespaces)
	for _, ns := range namespaces {
		suffix := strings.TrimPrefix(ns, prefix)
		suffix = strings.TrimPrefix(suffix, ".")
		if suffix == "" {
			dirs[ns] = targetDir
		} else {
			relPath := strings.ReplaceAll(suffix, ".", string(filepath.Separator))
			dirs[ns] = filepath.Join(targetDir, relPath)
		}
	}
	return dirs
}

// commonDotPrefix returns the longest common dot-separated prefix of the given strings.
func commonDotPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	parts := strings.Split(strs[0], ".")
	for _, s := range strs[1:] {
		sParts := strings.Split(s, ".")
		n := len(parts)
		if len(sParts) < n {
			n = len(sParts)
		}
		match := 0
		for i := 0; i < n; i++ {
			if parts[i] != sParts[i] {
				break
			}
			match = i + 1
		}
		parts = parts[:match]
	}
	return strings.Join(parts, ".")
}

// writeNamespaceIndex writes an _index.yaml and associated source files for one namespace.
func writeNamespaceIndex(dir, namespace string, entries []wapp.Entry) error {
	var entryNodes []*yaml.Node
	for _, entry := range entries {
		node, err := buildEntryNode(dir, entry)
		if err != nil {
			return err
		}
		entryNodes = append(entryNodes, node)
	}

	// Build the document manually to control field order
	root := &yaml.Node{Kind: yaml.MappingNode}
	addQuotedScalarPair(root, "version", "1.0")
	addScalarPair(root, "namespace", namespace)

	entriesKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "entries"}
	entriesSeq := &yaml.Node{Kind: yaml.SequenceNode, Content: entryNodes}
	root.Content = append(root.Content, entriesKey, entriesSeq)

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}

	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, "_index.yaml"), data, 0644)
}

// buildEntryNode creates a yaml.Node for a single entry, writing source files as needed.
func buildEntryNode(dir string, entry wapp.Entry) (*yaml.Node, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	addScalarPair(node, "name", entry.ID.Name)
	addScalarPair(node, "kind", entry.Kind)

	// Meta
	if len(entry.Meta) > 0 {
		metaKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "meta"}
		metaVal := &yaml.Node{}
		if err := metaVal.Encode(map[string]any(entry.Meta)); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, metaKey, metaVal)
	}

	dataMap, isMap := entry.Data.(map[string]any)
	if !isMap {
		if entry.Data != nil {
			dataKey := &yaml.Node{Kind: yaml.ScalarNode, Value: "data"}
			dataVal := &yaml.Node{}
			if err := dataVal.Encode(entry.Data); err != nil {
				return nil, err
			}
			node.Content = append(node.Content, dataKey, dataVal)
		}
		return node, nil
	}

	// Externalize the source field to a file if the entry kind supports it
	if ext := sourceExtForKind(entry.Kind); ext != "" {
		if src, ok := dataMap["source"].(string); ok && src != "" {
			srcFile := entry.ID.Name + ext
			if err := os.WriteFile(filepath.Join(dir, srcFile), []byte(src), 0644); err != nil {
				return nil, fmt.Errorf("write source file %s: %w", srcFile, err)
			}
			addScalarPair(node, "source", "file://"+srcFile)
			for _, k := range sortedKeys(dataMap) {
				if k == "source" {
					continue
				}
				if err := addAnyPair(node, k, dataMap[k]); err != nil {
					return nil, err
				}
			}
			return node, nil
		}
	}

	// Write all data fields inline
	for _, k := range sortedKeys(dataMap) {
		if err := addAnyPair(node, k, dataMap[k]); err != nil {
			return nil, err
		}
	}

	return node, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func addScalarPair(node *yaml.Node, key, value string) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

func addQuotedScalarPair(node *yaml.Node, key, value string) {
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value, Style: yaml.DoubleQuotedStyle},
	)
}

func addAnyPair(node *yaml.Node, key string, value any) error {
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	valNode := &yaml.Node{}
	if err := valNode.Encode(value); err != nil {
		return fmt.Errorf("encode yaml value for key %s: %w", key, err)
	}
	node.Content = append(node.Content, keyNode, valNode)
	return nil
}

// extractResourceFS writes all files from a resource filesystem to the target directory.
func extractResourceFS(targetDir string, resFS fs.ReadDirFS) error {
	return fs.WalkDir(resFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		outPath := filepath.Join(targetDir, path)
		if d.IsDir() {
			return os.MkdirAll(outPath, 0755)
		}

		f, err := resFS.Open(path)
		if err != nil {
			return fmt.Errorf("open resource file %s: %w", path, err)
		}
		defer f.Close()

		data, err := io.ReadAll(f)
		if err != nil {
			return fmt.Errorf("read resource file %s: %w", path, err)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(outPath, data, 0644)
	})
}
