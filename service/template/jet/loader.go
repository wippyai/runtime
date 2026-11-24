// SPDX-License-Identifier: MPL-2.0

package jet

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"

	"github.com/CloudyKit/jet/v6"
)

type templateLoader struct {
	*jet.InMemLoader
	active map[string]int
	// The root load holds mu until its reader closes. Nested loads share it.
	mu sync.Mutex
}

func (l *templateLoader) Open(path string) (io.ReadCloser, error) {
	root := !nestedTemplateLoad()
	if root {
		l.mu.Lock()
	} else if _, exists := l.active[path]; exists {
		chain := make([]string, len(l.active)+1)
		for name, index := range l.active {
			chain[index] = name
		}
		chain[len(chain)-1] = path
		return nil, fmt.Errorf("template dependency cycle: %s", strings.Join(chain, " -> "))
	}

	reader, err := l.InMemLoader.Open(path)
	if err != nil {
		if root {
			l.mu.Unlock()
		}
		return nil, err
	}
	// Jet resolves relative dependencies against the path passed to Open. Two
	// aliases of the same file can therefore resolve different dependencies.
	l.active[path] = len(l.active)
	return &templateReader{ReadCloser: reader, loader: l, path: path}, nil
}

// Jet uses the same Loader for parsing, execution-time lookups and direct
// callers of Set. A load nested under another loadFromFile belongs to its
// synchronous parse chain. The outer load keeps its reader open until parsing
// returns. A direct Parse has no reader of its own.
func nestedTemplateLoad() bool {
	const jetSet = "github.com/CloudyKit/jet/v6.(*Set)."
	var pcs [16]uintptr
	inLoad := false
	for skip := 2; ; {
		n := runtime.Callers(skip, pcs[:])
		frames := runtime.CallersFrames(pcs[:n])
		for {
			frame, more := frames.Next()
			switch frame.Function {
			case jetSet + "loadFromFile":
				if inLoad {
					return true
				}
				inLoad = true
			case jetSet + "GetTemplate", jetSet + "Parse", "github.com/CloudyKit/jet/v6.(*Runtime).executeInclude":
				return false
			}
			if !more {
				break
			}
		}
		if n < len(pcs) {
			return false
		}
		skip += n
	}
}

type templateReader struct {
	io.ReadCloser
	loader *templateLoader
	path   string
}

func (r *templateReader) Close() error {
	err := r.ReadCloser.Close()
	delete(r.loader.active, r.path)
	if len(r.loader.active) == 0 {
		r.loader.mu.Unlock()
	}
	return err
}
