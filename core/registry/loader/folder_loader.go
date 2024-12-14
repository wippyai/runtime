package loader

// import (
//
//	"fmt"
//	"github.com/ponyruntime/pony/internal/interpolator"
//	"os"
//	"path/filepath"
//	"strings"
//	"sync"
//
//	"github.com/ponyruntime/pony/api/payload"
//	"github.com/ponyruntime/pony/api/registry"
//	"go.uber.org/zap"
//
// )
//
// // FolderLoader manages the loading of registry entries from a directory.
//
//	type FolderLoader struct {
//		rootPath string
//		dtt      payload.Transcoder
//		mutex    *sync.RWMutex
//		log      *zap.Logger
//	}
type Variables map[string]string

//
//// NewFolderLoader creates a new FolderLoader.
//func NewFolderLoader(dtt payload.Transcoder, log *zap.Logger) *FolderLoader {
//	l := &FolderLoader{
//		dtt:   dtt,
//		mutex: &sync.RWMutex{},
//		log:   log,
//	}
//
//	if l.log == nil {
//		l.log = zap.NewNop()
//	}
//
//	return l
//}
//
//// Boot scans the root directory, loads all supported files, and returns a slice of entries
//func (l *FolderLoader) Boot(rootPath string, vars Variables) ([]registry.Entry, error) {
//	l.mutex.Lock()
//	defer l.mutex.Unlock()
//
//	l.rootPath = rootPath // Store the root path
//
//	fileReadFunc := l.createFileReadFunc(rootPath)
//	entryLoader := NewEntryLoader(l.log) // Pass fileReadFunc
//	payloads, err := entryLoader.Load(rootPath)
//	if err != nil {
//		return nil, fmt.Errorf("failed to load entries: %w", err)
//	}
//
//	intr := interpolator.NewInterpolator(vars, fileReadFunc) // Initialize Interpolator here
//
//	entries := make([]registry.Entry, 0)
//
//	for relPath, p := range payloads {
//		// Interpolate the payload before registering
//		interpolatedPayload, err := intr.Interpolate(p, l.dtt)
//		if err != nil {
//			l.log.Error("failed to interpolate payload", zap.String("path", relPath), zap.Error(err))
//			continue
//		}
//
//		entry, err := l.register(interpolatedPayload, relPath) // Use interpolated payload
//		if err != nil {
//			l.log.Error("failed to register entry", zap.String("path", relPath), zap.Error(err))
//			continue // Skip this entry
//		}
//		entries = append(entries, entry)
//	}
//
//	return entries, nil
//}
//
//// register processes the payloads and extracts configuration entries.
//func (l *FolderLoader) register(p payload.Payload, relPath string) (registry.Entry, error) {
//	var entry fileEntry
//	err := l.dtt.Unmarshal(p, &entry)
//	if err != nil {
//		return registry.Entry{}, fmt.Errorf("failed to unmarshal payload as registry.Entry: %w", err)
//	}
//
//	if entry.Name == "" {
//		return registry.Entry{}, fmt.Errorf("missing Name in registry entry")
//	}
//
//	if entry.Kind == "" {
//		return registry.Entry{}, fmt.Errorf("missing Kind in registry entry")
//	}
//
//	// Calculate full ID (prefix + entry path)
//	fullID := l.calculateFullID(relPath, entry.Name)
//
//	return registry.Entry{
//		Path: fullID,
//		Kind: entry.Kind,
//		Meta: entry.Meta,
//		Data: p,
//	}, nil
//}
//
//// Reset clears all loaded entries.
//func (l *FolderLoader) Reset() {
//	l.mutex.Lock()
//	defer l.mutex.Unlock()
//	// No need to clear entries here, as Boot rebuilds the ChangeSet
//}
//
//// calculateFullID determines the full registry path based on file path, and entry path.
//func (l *FolderLoader) calculateFullID(relPath string, entryName string) registry.Path {
//	// we never store filename
//	relPath = strings.TrimSuffix(relPath, filepath.Base(relPath))
//
//	fullID := entryName
//	if relPath != "" {
//		fullID = strings.TrimSuffix(relPath, "/") + "." + entryName // Add "." to separate
//	}
//
//	return registry.Path(strings.ReplaceAll(fullID, "/", "."))
//}
//
//// fileEntry is an internal struct used for unmarshalling entry data.
//type fileEntry struct {
//	Name string            `json:"name" yaml:"name"`
//	Kind registry.Kind     `json:"kind" yaml:"kind"`
//	Meta registry.Metadata `json:"meta" yaml:"meta"`
//}
//
//// createFileReadFunc creates a function that reads files relative to a given directory.
//func (l *FolderLoader) createFileReadFunc(baseDir string) func(string) (string, error) {
//	return func(filePath string) (string, error) {
//		// Join the base directory with the requested file path
//		fullPath := filepath.Join(baseDir, filePath)
//
//		// Make sure the path is still within the root directory (security check)
//		relPath, err := filepath.Rel(l.rootPath, fullPath)
//		if err != nil || strings.HasPrefix(relPath, "..") {
//			return "", fmt.Errorf("file path '%s' is outside of the root directory", filePath)
//		}
//
//		data, err := os.ReadFile(fullPath)
//		if err != nil {
//			return "", err
//		}
//		return string(data), nil
//	}
//}
