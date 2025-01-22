package payload

import (
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/internal/graph"
)

// Transcoder is the global instance of the json service.
type Transcoder struct {
	graph           *graph.Graph
	transcoders     map[graph.Node]map[graph.Node]payload.FormatTranscoder
	unmarshalers    map[graph.Node]payload.Unmarshaler
	unmarshalerPath sync.Map // thread-safe cache for unmarshaler paths
	transcodePath   sync.Map // thread-safe cache for transcoder paths
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
		graph:        graph.NewGraph(),
		transcoders:  make(map[graph.Node]map[graph.Node]payload.FormatTranscoder),
		unmarshalers: make(map[graph.Node]payload.Unmarshaler),
	}
}

// RegisterTranscoder registers a transcoder for a specific format conversion.
// Expected to be called only during initialization.
func (t *Transcoder) RegisterTranscoder(from, to payload.Format, weight int, tt payload.FormatTranscoder) {
	fromNode := graph.Node(from)
	toNode := graph.Node(to)

	t.graph.AddNode(fromNode)
	t.graph.AddNode(toNode)
	t.graph.AddEdge(graph.Edge{From: fromNode, To: toNode, Weight: weight})

	if _, ok := t.transcoders[fromNode]; !ok {
		t.transcoders[fromNode] = make(map[graph.Node]payload.FormatTranscoder)
	}

	t.transcoders[fromNode][toNode] = tt
}

// RegisterUnmarshaler registers an unmarshaler from a specific format.
// Expected to be called only during initialization.
func (t *Transcoder) RegisterUnmarshaler(from payload.Format, unmarshaler payload.Unmarshaler) {
	formatNode := graph.Node(from)
	t.graph.AddNode(formatNode)
	t.unmarshalers[formatNode] = unmarshaler
}

// getTranscodePath returns the cached path or computes and caches a new path
func (t *Transcoder) getTranscodePath(from, to graph.Node) (*graph.Path, error) {
	cacheKey := fmt.Sprintf("%s:%s", from, to)

	// Fast path: check if path is already cached
	if path, ok := t.transcodePath.Load(cacheKey); ok {
		return path.(*graph.Path), nil
	}

	// Slow path: compute path and cache it
	path, err := t.graph.ShortestPath(from, to)
	if err != nil {
		return nil, fmt.Errorf("no transcoding path found from %s to %s", from, to)
	}

	// Store computed path in cache
	t.transcodePath.Store(cacheKey, path)
	return path, nil
}

// Transcode transcodes a payload to a different format.
func (t *Transcoder) Transcode(p payload.Payload, to payload.Format) (payload.Payload, error) {
	if p.Format() == to {
		return p, nil
	}

	fromNode := graph.Node(p.Format())
	toNode := graph.Node(to)

	path, err := t.getTranscodePath(fromNode, toNode)
	if err != nil {
		return nil, err
	}

	currentPayload := p
	for i := 0; i < len(path.Nodes)-1; i++ {
		currentFrom := path.Nodes[i]
		currentTo := path.Nodes[i+1]

		tt, ok := t.transcoders[currentFrom][currentTo]
		if !ok || tt == nil {
			return nil, fmt.Errorf("no transcoder registered for %s to %s", currentFrom, currentTo)
		}

		var err error
		currentPayload, err = tt.Transcode(currentPayload)
		if err != nil {
			return nil, fmt.Errorf("error transcoding from %s to %s: %w", currentFrom, currentTo, err)
		}
	}

	return currentPayload, nil
}

// findUnmarshalPath finds the shortest path from a given format to a format that has an associated unmarshaler.
func (t *Transcoder) findUnmarshalPath(from graph.Node) (*graph.Path, error) {
	// Fast path: check if path is already cached
	if path, ok := t.unmarshalerPath.Load(from); ok {
		return path.(*graph.Path), nil
	}

	// Slow path: compute path
	var unmarshalPath *graph.Path
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
		return nil, fmt.Errorf("no unmarshaling path found for format %s", from)
	}

	// Store computed path in cache
	t.unmarshalerPath.Store(from, unmarshalPath)
	return unmarshalPath, nil
}

// Unmarshal unmarshals a payload into a given struct.
func (t *Transcoder) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() == "" {
		return fmt.Errorf("payload format is empty")
	}

	fromNode := graph.Node(p.Format())

	// Check if the current format has a direct unmarshaler
	unmarshaler, ok := t.unmarshalers[fromNode]
	if ok {
		return unmarshaler.Unmarshal(p, v)
	}

	path, err := t.findUnmarshalPath(fromNode)
	if err != nil {
		return err
	}

	transcodedPayload, err := t.Transcode(p, payload.Format(path.Nodes[len(path.Nodes)-1]))
	if err != nil {
		return fmt.Errorf("error transcoding payload for unmarshaling: %w", err)
	}

	unmarshaler, ok = t.unmarshalers[path.Nodes[len(path.Nodes)-1]]
	if !ok {
		return fmt.Errorf("unmarshaler not found for format %s, even though a path was found", path.Nodes[len(path.Nodes)-1])
	}

	return unmarshaler.Unmarshal(transcodedPayload, v)
}
