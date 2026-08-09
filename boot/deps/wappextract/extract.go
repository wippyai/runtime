// SPDX-License-Identifier: MPL-2.0

package wappextract

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wippyai/wapp"
	"gopkg.in/yaml.v3"
)

// ExtractWappToDir extracts a .wapp file into a source directory with
// _index.yaml files and source files. After extraction, the .wapp file is
// removed.
func ExtractWappToDir(wappPath, targetDir string) error {
	return extractWappToDir(wappPath, targetDir, true)
}

// ExtractWappToDirKeepSource extracts a .wapp file without removing the
// source .wapp. Use this when another step must complete before the packed
// artifact can be safely discarded.
func ExtractWappToDirKeepSource(wappPath, targetDir string) error {
	return extractWappToDir(wappPath, targetDir, false)
}

func extractWappToDir(wappPath, targetDir string, removeSource bool) error {
	if targetDir == "" {
		return fmt.Errorf("target directory is empty")
	}
	parent := filepath.Dir(targetDir)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create target parent: %w", err)
	}
	tmpDir, err := os.MkdirTemp(parent, "."+filepath.Base(targetDir)+".extract-*")
	if err != nil {
		return fmt.Errorf("create temporary extraction directory: %w", err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := extractWappToDirContents(wappPath, tmpDir); err != nil {
		return err
	}
	if err := replaceDirectory(targetDir, tmpDir); err != nil {
		return err
	}
	cleanupTmp = false
	if removeSource {
		if err := os.Remove(wappPath); err != nil {
			return fmt.Errorf("remove wapp file: %w", err)
		}
	}
	return nil
}

func extractWappToDirContents(wappPath, targetDir string) error {
	file, err := os.Open(wappPath)
	if err != nil {
		return fmt.Errorf("open wapp file: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()

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

	entries, resources, err = restoreEmbeddedResources(restoreInput{
		Entries:   entries,
		Resources: resources,
		TargetDir: targetDir,
	})
	if err != nil {
		return err
	}

	grouped := make(map[string][]wapp.Entry)
	var namespaces []string
	for _, entry := range entries {
		ns := entry.ID.Namespace
		if _, seen := grouped[ns]; !seen {
			namespaces = append(namespaces, ns)
		}
		grouped[ns] = append(grouped[ns], entry)
	}

	nsDirs, err := resolveNamespaceDirs(targetDir, namespaces)
	if err != nil {
		return err
	}
	for _, ns := range namespaces {
		nsDir := nsDirs[ns]
		if err := os.MkdirAll(nsDir, 0755); err != nil {
			return fmt.Errorf("create namespace directory: %w", err)
		}
		if err := writeNamespaceIndex(nsDir, ns, grouped[ns]); err != nil {
			return fmt.Errorf("write index for namespace %s: %w", ns, err)
		}
	}

	for _, res := range resources {
		if err := extractResourceFS(targetDir, res.fs); err != nil {
			return fmt.Errorf("extract resource %s: %w", res.id, err)
		}
	}

	err = file.Close()
	closed = true
	if err != nil {
		return fmt.Errorf("close wapp file: %w", err)
	}
	return nil
}

func replaceDirectory(targetDir, replacementDir string) error {
	parent := filepath.Dir(targetDir)
	base := filepath.Base(targetDir)
	var backupDir string

	if _, err := os.Stat(targetDir); err == nil {
		var mkErr error
		backupDir, mkErr = os.MkdirTemp(parent, "."+base+".backup-*")
		if mkErr != nil {
			return fmt.Errorf("create backup directory: %w", mkErr)
		}
		if err := os.Remove(backupDir); err != nil {
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("prepare backup directory: %w", err)
		}
		if err := os.Rename(targetDir, backupDir); err != nil {
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("move existing directory aside: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat target directory: %w", err)
	}

	if err := os.Rename(replacementDir, targetDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, targetDir)
		}
		return fmt.Errorf("activate extracted directory: %w", err)
	}

	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

type extractedResource struct {
	fs   fs.ReadDirFS
	nsID wapp.ID
	id   string
}

type restoreInput struct {
	TargetDir string
	Entries   []wapp.Entry
	Resources []extractedResource
}

func restoreEmbeddedResources(in restoreInput) ([]wapp.Entry, []extractedResource, error) {
	if len(in.Resources) == 0 {
		return in.Entries, in.Resources, nil
	}

	resMap := make(map[string]int, len(in.Resources))
	for i, res := range in.Resources {
		resMap[res.id] = i
	}

	claimed := make(map[int]bool)
	result := make([]wapp.Entry, len(in.Entries))
	for i, entry := range in.Entries {
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

		entryName, err := safePathSegment("entry name", entry.ID.Name)
		if err != nil {
			return nil, nil, err
		}
		resDir, err := safeJoinSegments(in.TargetDir, entryName)
		if err != nil {
			return nil, nil, err
		}
		if err := os.MkdirAll(resDir, 0755); err != nil {
			return nil, nil, fmt.Errorf("create resource directory %s: %w", entry.ID.Name, err)
		}
		if err := extractResourceFS(resDir, in.Resources[resIdx].fs); err != nil {
			return nil, nil, fmt.Errorf("extract embedded resource %s: %w", entryKey, err)
		}

		result[i] = wapp.Entry{
			ID:   entry.ID,
			Kind: "fs.directory",
			Meta: entry.Meta,
			Data: map[string]any{
				"directory": entryName,
				"base":      "module",
			},
		}
		claimed[resIdx] = true
	}

	var remaining []extractedResource
	for i, res := range in.Resources {
		if !claimed[i] {
			remaining = append(remaining, res)
		}
	}
	return result, remaining, nil
}

func resolveNamespaceDirs(targetDir string, namespaces []string) (map[string]string, error) {
	dirs := make(map[string]string, len(namespaces))
	if len(namespaces) <= 1 {
		for _, ns := range namespaces {
			dirs[ns] = targetDir
		}
		return dirs, nil
	}

	prefix := commonDotPrefix(namespaces)
	for _, ns := range namespaces {
		suffix := strings.TrimPrefix(ns, prefix)
		suffix = strings.TrimPrefix(suffix, ".")
		if suffix == "" {
			dirs[ns] = targetDir
		} else {
			nsDir, err := namespaceDir(targetDir, suffix)
			if err != nil {
				return nil, fmt.Errorf("resolve namespace directory %s: %w", ns, err)
			}
			dirs[ns] = nsDir
		}
	}
	return dirs, nil
}

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

func writeNamespaceIndex(dir, namespace string, entries []wapp.Entry) error {
	var entryNodes []*yaml.Node
	for _, entry := range entries {
		node, err := buildEntryNode(dir, entry)
		if err != nil {
			return err
		}
		entryNodes = append(entryNodes, node)
	}

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
	indexPath, err := safeJoinSegments(dir, "_index.yaml")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, data, 0644)
}

func buildEntryNode(dir string, entry wapp.Entry) (*yaml.Node, error) {
	entryName, err := safePathSegment("entry name", entry.ID.Name)
	if err != nil {
		return nil, err
	}

	node := &yaml.Node{Kind: yaml.MappingNode}
	addScalarPair(node, "name", entryName)
	addScalarPair(node, "kind", entry.Kind)

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

	if ext := sourceExtForKind(entry.Kind); ext != "" {
		if src, ok := dataMap["source"].(string); ok && src != "" {
			srcFile := entryName + ext
			srcPath, err := safeJoinSegments(dir, srcFile)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
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

func extractResourceFS(targetDir string, resFS fs.ReadDirFS) error {
	return fs.WalkDir(resFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		outPath, err := safeResourcePath(targetDir, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(outPath, 0755)
		}

		f, err := resFS.Open(path)
		if err != nil {
			return fmt.Errorf("open resource file %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			_ = f.Close()
			return err
		}
		// Keep extracted resource files readable like the previous os.WriteFile path.
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644) //nolint:gosec
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("create resource file %s: %w", path, err)
		}

		_, copyErr := io.Copy(out, f)
		closeOutErr := out.Close()
		closeInErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("copy resource file %s: %w", path, copyErr)
		}
		if closeOutErr != nil {
			return fmt.Errorf("close extracted resource file %s: %w", path, closeOutErr)
		}
		if closeInErr != nil {
			return fmt.Errorf("close resource file %s: %w", path, closeInErr)
		}
		return nil
	})
}

func namespaceDir(targetDir, suffix string) (string, error) {
	parts := strings.Split(suffix, ".")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segment, err := safePathSegment("namespace segment", part)
		if err != nil {
			return "", err
		}
		segments = append(segments, segment)
	}
	return safeJoinSegments(targetDir, segments...)
}

func safeResourcePath(targetDir, resourcePath string) (string, error) {
	if resourcePath == "." {
		return targetDir, nil
	}
	if !fs.ValidPath(resourcePath) || pathpkg.IsAbs(resourcePath) || strings.Contains(resourcePath, `\`) {
		return "", fmt.Errorf("unsafe resource path %q", resourcePath)
	}
	return safeJoinSegments(targetDir, strings.Split(resourcePath, "/")...)
}

func safeJoinSegments(base string, segments ...string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("base directory is empty")
	}

	cleanBase := filepath.Clean(base)
	parts := make([]string, 0, len(segments)+1)
	parts = append(parts, cleanBase)
	for _, segment := range segments {
		safeSegment, err := safePathSegment("path segment", segment)
		if err != nil {
			return "", err
		}
		parts = append(parts, safeSegment)
	}

	outPath := filepath.Join(parts...)
	if err := ensureWithinBase(cleanBase, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

func safePathSegment(label, segment string) (string, error) {
	if segment == "" || segment == "." || segment == ".." {
		return "", fmt.Errorf("unsafe %s %q", label, segment)
	}
	if filepath.IsAbs(segment) || strings.ContainsAny(segment, "/\\:\x00") {
		return "", fmt.Errorf("unsafe %s %q", label, segment)
	}
	if filepath.Clean(segment) != segment {
		return "", fmt.Errorf("unsafe %s %q", label, segment)
	}
	return segment, nil
}

func ensureWithinBase(base, candidate string) error {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return fmt.Errorf("resolve base path: %w", err)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	rel, err := filepath.Rel(absBase, absCandidate)
	if err != nil {
		return fmt.Errorf("compare output path: %w", err)
	}
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("output path escapes target directory: %s", candidate)
	}
	return nil
}

type sourceKind struct {
	ext string
}

var sourceKinds = map[string]sourceKind{
	"function.lua": {ext: ".lua"},
	"library.lua":  {ext: ".lua"},
	"process.lua":  {ext: ".lua"},
	"workflow.lua": {ext: ".lua"},
	"template.jet": {ext: ".jet"},
}

func sourceExtForKind(kind string) string {
	if sk, ok := sourceKinds[kind]; ok {
		return sk.ext
	}
	if idx := strings.LastIndex(kind, "."); idx >= 0 {
		suffix := kind[idx:]
		for _, sk := range sourceKinds {
			if sk.ext == suffix {
				return sk.ext
			}
		}
	}
	return ""
}
