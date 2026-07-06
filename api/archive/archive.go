// SPDX-License-Identifier: MPL-2.0

// Package archive defines the codec contracts and registry used by the Lua
// archive module to read and write zip/tar archives with bounded memory.
package archive

import (
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry describes a single member of an archive. Sizes are uncompressed.
type Entry struct {
	Modified       time.Time
	Name           string
	Method         string
	Size           int64
	CompressedSize int64
	Mode           fs.FileMode
	CRC32          uint32
	IsDir          bool
}

// Options carries the bounded-memory and safety limits for an open/create call.
type Options struct {
	Format         string
	MaxEntries     int
	MaxTotalBytes  int64
	MaxFileBytes   int64
	MaxInlineBytes int64
	BufferBytes    int
}

// Reader is random access over a seekable archive source. Open returns an
// independent streaming reader per call; nothing materializes a whole entry.
type Reader interface {
	Entries() []Entry
	Stat(name string) (Entry, bool)
	Open(name string) (io.ReadCloser, Entry, error)
	Close() error
}

// Walker is forward-only access over a streamed archive source. The reader from
// Next is valid only until the following Next call.
type Walker interface {
	Next() (Entry, io.Reader, error)
	Close() error
}

// Writer streams new entries into an archive. Create returns a writer for the
// entry body; the caller streams into it before the next Create.
type Writer interface {
	Create(e Entry) (io.Writer, error)
	Close() error
}

// Codec identifies a container format and detects it from a header.
type Codec interface {
	Name() string
	Extensions() []string
	Sniff(header []byte) bool
}

// RandomReadable is a codec that supports random access over a seekable source.
type RandomReadable interface {
	Codec
	OpenRandom(r io.ReaderAt, size int64, o Options) (Reader, error)
}

// StreamReadable is a codec that supports forward-only reading.
type StreamReadable interface {
	Codec
	OpenStream(r io.Reader, o Options) (Walker, error)
}

// Writable is a codec that supports streaming creation.
type Writable interface {
	Codec
	OpenWriter(w io.Writer, o Options) (Writer, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Codec{}
)

// Register adds a codec to the global registry. Intended for use from init().
func Register(c Codec) {
	mu.Lock()
	defer mu.Unlock()
	registry[c.Name()] = c
}

// Get returns the codec registered under name.
func Get(name string) (Codec, bool) {
	mu.RLock()
	defer mu.RUnlock()
	c, ok := registry[name]
	return c, ok
}

// List returns the registered codec names, sorted.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Resolve picks a codec by explicit name, then by sniffing the header, then by
// the source name's extension. ok is false when none matches.
func Resolve(format, sourceName string, header []byte) (Codec, bool) {
	if format != "" {
		c, ok := Get(format)
		return c, ok
	}

	mu.RLock()
	defer mu.RUnlock()

	if len(header) > 0 {
		for _, name := range sortedNames() {
			if registry[name].Sniff(header) {
				return registry[name], true
			}
		}
	}

	lower := strings.ToLower(sourceName)
	var best Codec
	var bestLen int
	for _, name := range sortedNames() {
		for _, ext := range registry[name].Extensions() {
			if strings.HasSuffix(lower, ext) && len(ext) > bestLen {
				best = registry[name]
				bestLen = len(ext)
			}
		}
	}
	return best, best != nil
}

// sortedNames returns registry keys sorted; callers must hold mu.
func sortedNames() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
