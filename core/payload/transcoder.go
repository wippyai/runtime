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
	transcoders     map[graph.Node]map[graph.Node]payload.FormatTranscoder // from -> to -> via transcoder
	unmarshalers    map[graph.Node]payload.Unmarshaler                     // format -> unmarshaler
	mutex           sync.RWMutex
	unmarshalerPath map[graph.Node]*graph.Path // Cache for unmarshaler paths
}

// globalTranscoder is the singleton instance of Transcoder.
var globalTranscoder *Transcoder
var once sync.Once

// GlobalTranscoder returns the singleton instance of Transcoder.
func GlobalTranscoder() *Transcoder {
	once.Do(func() {
		globalTranscoder = NewTranscoder()
	})
	return globalTranscoder
}

func NewTranscoder() *Transcoder {
	return &Transcoder{
		graph:           graph.NewGraph(),
		transcoders:     make(map[graph.Node]map[graph.Node]payload.FormatTranscoder),
		unmarshalers:    make(map[graph.Node]payload.Unmarshaler),
		unmarshalerPath: make(map[graph.Node]*graph.Path),
	}
}

// RegisterTranscoder registers a json for a specific format conversion.
func (t *Transcoder) RegisterTranscoder(from, to payload.Format, weight int, tt payload.FormatTranscoder) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	fromNode := graph.Node(from)
	toNode := graph.Node(to)

	t.graph.AddNode(fromNode)
	t.graph.AddNode(toNode)
	t.graph.AddEdge(graph.Edge{From: fromNode, To: toNode, Weight: weight})

	if _, ok := t.transcoders[fromNode]; !ok {
		t.transcoders[fromNode] = make(map[graph.Node]payload.FormatTranscoder)
	}

	t.transcoders[fromNode][toNode] = tt

	// Invalidate unmarshaler path cache as the graph has changed
	t.unmarshalerPath = make(map[graph.Node]*graph.Path)
}

// RegisterUnmarshaler registers an unmarshaler from a specific format.
func (t *Transcoder) RegisterUnmarshaler(from payload.Format, unmarshaler payload.Unmarshaler) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	formatNode := graph.Node(from)
	t.graph.AddNode(formatNode)
	t.unmarshalers[formatNode] = unmarshaler

	// Invalidate unmarshaler path cache as the graph has changed
	t.unmarshalerPath = make(map[graph.Node]*graph.Path)
}

// Transcode transcodes a payload to a different format.
func (t *Transcoder) Transcode(p payload.Payload, to payload.Format) (payload.Payload, error) {
	if p.Format() == to {
		return p, nil
	}

	t.mutex.RLock()
	defer t.mutex.RUnlock()

	fromNode := graph.Node(p.Format())
	toNode := graph.Node(to)

	path, err := t.graph.ShortestPath(fromNode, toNode)
	if err != nil {
		return nil, fmt.Errorf("no transcoding path found from %s to %s", fromNode, toNode)
	}

	currentPayload := p
	for i := 0; i < len(path.Nodes)-1; i++ {
		currentFrom := path.Nodes[i]
		currentTo := path.Nodes[i+1]

		tt, ok := t.transcoders[currentFrom][currentTo]
		if !ok || tt == nil {
			return nil, fmt.Errorf("no json registered for %s to %s", currentFrom, currentTo)
		}

		currentPayload, err = tt.Transcode(currentPayload)
		if err != nil {
			return nil, fmt.Errorf("error transcoding from %s to %s: %w", currentFrom, currentTo, err)
		}
	}

	return currentPayload, nil
}

// findUnmarshalPath finds the shortest path from a given format to a format that has an associated unmarshaler.
func (t *Transcoder) findUnmarshalPath(from graph.Node) (*graph.Path, error) {
	// 1. Check the cache with a read lock first.
	t.mutex.RLock()
	cachedPath, ok := t.unmarshalerPath[from]
	t.mutex.RUnlock() // Release the read lock immediately.

	if ok {
		return cachedPath, nil
	}

	// 2. If not in the cache, acquire a write lock to search and potentially update the cache.
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// 3. Double-check the cache after acquiring the write lock (in case another goroutine updated it).
	cachedPath, ok = t.unmarshalerPath[from]
	if ok {
		return cachedPath, nil
	}

	// 4. Search for the shortest path.
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

	// 5. Cache the path.
	t.unmarshalerPath[from] = unmarshalPath

	return unmarshalPath, nil
}

// Unmarshal unmarshals a payload into a given struct.
func (t *Transcoder) Unmarshal(p payload.Payload, v interface{}) error {
	fromNode := graph.Node(p.Format())

	// Check if the current format has a direct unmarshaler
	t.mutex.RLock()
	unmarshaler, ok := t.unmarshalers[fromNode]
	t.mutex.RUnlock()
	if ok {
		return unmarshaler.Unmarshal(p, v)
	}
	// Find a path to a format with an unmarshaler
	path, err := t.findUnmarshalPath(fromNode)
	if err != nil {
		return err
	}

	// Transcode to the unmarshaler format
	transcodedPayload, err := t.Transcode(p, payload.Format(path.Nodes[len(path.Nodes)-1]))
	if err != nil {
		return fmt.Errorf("error transcoding payload for unmarshaling: %w", err)
	}

	// Unmarshal using the found unmarshaler
	t.mutex.RLock()
	unmarshaler, ok = t.unmarshalers[path.Nodes[len(path.Nodes)-1]]
	t.mutex.RUnlock()
	if !ok {
		return fmt.Errorf("unmarshaler not found for format %s, even though a path was found", path.Nodes[len(path.Nodes)-1]) // Should not happen
	}

	return unmarshaler.Unmarshal(transcodedPayload, v)
}
