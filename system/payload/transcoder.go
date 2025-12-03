package payload

import (
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/internal/graph"
)

// pathKey is a cache key for transcoding paths (avoids string concatenation)
type pathKey struct {
	from, to string
}

// Transcoder handles payload format conversions using a graph-based routing system.
type Transcoder struct {
	graph           *graph.Graph[string, any]
	transcoders     map[string]map[string]payload.FormatTranscoder
	unmarshalers    map[string]payload.Unmarshaler
	unmarshalerPath *sync.Map // map[string]*graph.Path[string]
	transcodePath   *sync.Map // map[pathKey]*graph.Path[string]
}

var globalTranscoder *Transcoder
var once sync.Once

// GlobalTranscoder returns the global static transcoder instance.
func GlobalTranscoder() *Transcoder {
	once.Do(func() {
		globalTranscoder = NewTranscoder()
	})
	return globalTranscoder
}

// NewTranscoder creates a new transcoder instance.
func NewTranscoder() *Transcoder {
	return &Transcoder{
		graph:           graph.New[string, any](),
		transcoders:     make(map[string]map[string]payload.FormatTranscoder),
		unmarshalers:    make(map[string]payload.Unmarshaler),
		unmarshalerPath: new(sync.Map),
		transcodePath:   new(sync.Map),
	}
}

// RegisterTranscoder registers a transcoder for a specific format conversion.
// Expected to be called only during initialization.
func (t *Transcoder) RegisterTranscoder(from, to payload.Format, weight int, tt payload.FormatTranscoder) {
	fromStr := string(from)
	toStr := string(to)

	t.graph.AddNode(fromStr)
	t.graph.AddNode(toStr)
	t.graph.AddEdge(fromStr, toStr, weight, nil)

	if _, ok := t.transcoders[fromStr]; !ok {
		t.transcoders[fromStr] = make(map[string]payload.FormatTranscoder)
	}

	t.transcoders[fromStr][toStr] = tt
}

// RegisterUnmarshaler registers an unmarshaler from a specific format.
// Expected to be called only during initialization.
func (t *Transcoder) RegisterUnmarshaler(from payload.Format, unmarshaler payload.Unmarshaler) {
	formatStr := string(from)
	t.graph.AddNode(formatStr)
	t.unmarshalers[formatStr] = unmarshaler
}

// getTranscodePath returns the cached path or computes and caches a new path
func (t *Transcoder) getTranscodePath(from, to string) (*graph.Path[string], error) {
	key := pathKey{from, to}

	// Fast path: check if path is already cached
	if path, ok := t.transcodePath.Load(key); ok {
		return path.(*graph.Path[string]), nil
	}

	// Slow path: compute path and cache it
	path, err := t.graph.ShortestPath(from, to)
	if err != nil {
		return nil, payload.NewNoTranscodingPathError(payload.Format(from), payload.Format(to))
	}

	// Store computed path in cache
	t.transcodePath.Store(key, path)
	return path, nil
}

// Transcode transcodes a payload to a different format.
func (t *Transcoder) Transcode(p payload.Payload, to payload.Format) (payload.Payload, error) {
	if p.Format() == to {
		return p, nil
	}

	fromStr := string(p.Format())
	toStr := string(to)

	path, err := t.getTranscodePath(fromStr, toStr)
	if err != nil {
		return nil, err
	}

	currentPayload := p
	for i := 0; i < len(path.Nodes)-1; i++ {
		currentFrom := path.Nodes[i]
		currentTo := path.Nodes[i+1]

		tt, ok := t.transcoders[currentFrom][currentTo]
		if !ok || tt == nil {
			return nil, payload.NewNoTranscoderError(currentFrom, currentTo)
		}

		currentPayload, err = tt.Transcode(currentPayload)
		if err != nil {
			return nil, payload.NewTranscodeError(currentFrom, currentTo, err)
		}
	}

	return currentPayload, nil
}

// findUnmarshalPath finds the shortest path from a given format to a format that has an associated unmarshaler.
func (t *Transcoder) findUnmarshalPath(from string) (*graph.Path[string], error) {
	// Fast path: check if path is already cached
	if path, ok := t.unmarshalerPath.Load(from); ok {
		return path.(*graph.Path[string]), nil
	}

	// Slow path: compute path
	var unmarshalPath *graph.Path[string]
	minCost := -1

	for unmarshalerFormat := range t.unmarshalers {
		path, err := t.graph.ShortestPath(from, unmarshalerFormat)
		if err == nil {
			if minCost == -1 || path.Cost < minCost {
				minCost = path.Cost
				unmarshalPath = path
			}
		}
	}

	if unmarshalPath == nil {
		return nil, payload.NewNoUnmarshalPathError(payload.Format(from))
	}

	// Store computed path in cache
	t.unmarshalerPath.Store(from, unmarshalPath)
	return unmarshalPath, nil
}

// Unmarshal unmarshals a payload into a given struct.
func (t *Transcoder) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() == "" {
		return payload.ErrEmptyFormat
	}

	fromStr := string(p.Format())

	// Check if the current format has a direct unmarshaler
	unmarshaler, ok := t.unmarshalers[fromStr]
	if ok {
		return unmarshaler.Unmarshal(p, v)
	}

	path, err := t.findUnmarshalPath(fromStr)
	if err != nil {
		return err
	}

	transcodedPayload, err := t.Transcode(p, payload.Format(path.Nodes[len(path.Nodes)-1]))
	if err != nil {
		return payload.NewUnmarshalTranscodeError(err)
	}

	unmarshaler, ok = t.unmarshalers[path.Nodes[len(path.Nodes)-1]]
	if !ok {
		return payload.NewUnmarshalerNotFoundError(path.Nodes[len(path.Nodes)-1])
	}

	return unmarshaler.Unmarshal(transcodedPayload, v)
}
