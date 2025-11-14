package payload

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/internal/graph"
	"go.uber.org/zap"
)

// Transcoder is the global instance of the json service.
type Transcoder struct {
	graph           *graph.Graph[string, any]
	transcoders     map[string]map[string]payload.FormatTranscoder
	unmarshalers    map[string]payload.Unmarshaler
	unmarshalerPath *sync.Map // thread-safe cache for unmarshaler paths
	transcodePath   *sync.Map // thread-safe cache for transcoder paths
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
	cacheKey := fmt.Sprintf("%s:%s", from, to)

	// Fast path: check if path is already cached
	if path, ok := t.transcodePath.Load(cacheKey); ok {
		return path.(*graph.Path[string]), nil
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
	// Get logger from context if available
	logger := logs.GetLogger(context.Background())

	if p.Format() == to {
		logger.Info("Transcoder.Transcode - same format, no transcoding needed",
			zap.String("format", string(p.Format())),
		)
		return p, nil
	}

	fromStr := string(p.Format())
	toStr := string(to)

	// TODO-CLD: Remove The logger from this file entirely
	logger.Info("Transcoder.Transcode - starting transcoding",
		zap.String("from_format", fromStr),
		zap.String("to_format", toStr),
		zap.String("payload_data_type", fmt.Sprintf("%T", p.Data())),
	)

	path, err := t.getTranscodePath(fromStr, toStr)
	if err != nil {
		logger.Error("Transcoder.Transcode - no transcoding path found",
			zap.String("from_format", fromStr),
			zap.String("to_format", toStr),
			zap.Error(err),
		)
		return nil, err
	}

	logger.Info("Transcoder.Transcode - found transcoding path",
		zap.String("from_format", fromStr),
		zap.String("to_format", toStr),
		zap.Int("path_length", len(path.Nodes)),
		zap.Int("path_cost", path.Cost),
		zap.Strings("path_nodes", path.Nodes),
	)

	currentPayload := p
	for i := 0; i < len(path.Nodes)-1; i++ {
		currentFrom := path.Nodes[i]
		currentTo := path.Nodes[i+1]

		logger.Info("Transcoder.Transcode - transcoding step",
			zap.Int("step", i+1),
			zap.String("from", currentFrom),
			zap.String("to", currentTo),
		)

		tt, ok := t.transcoders[currentFrom][currentTo]
		if !ok || tt == nil {
			logger.Error("Transcoder.Transcode - no transcoder registered for step",
				zap.Int("step", i+1),
				zap.String("from", currentFrom),
				zap.String("to", currentTo),
			)
			return nil, fmt.Errorf("no transcoder registered for %s to %s", currentFrom, currentTo)
		}

		logger.Info("Transcoder.Transcode - using transcoder",
			zap.Int("step", i+1),
			zap.String("from", currentFrom),
			zap.String("to", currentTo),
			zap.String("transcoder_type", fmt.Sprintf("%T", tt)),
		)

		var err error
		currentPayload, err = tt.Transcode(currentPayload)
		if err != nil {
			logger.Error("Transcoder.Transcode - transcoding step failed",
				zap.Int("step", i+1),
				zap.String("from", currentFrom),
				zap.String("to", currentTo),
				zap.Error(err),
			)
			return nil, fmt.Errorf("error transcoding from %s to %s: %w", currentFrom, currentTo, err)
		}

		logger.Info("Transcoder.Transcode - transcoding step successful",
			zap.Int("step", i+1),
			zap.String("from", currentFrom),
			zap.String("to", currentTo),
			zap.String("result_format", string(currentPayload.Format())),
		)
	}

	logger.Info("Transcoder.Transcode - transcoding completed successfully",
		zap.String("from_format", fromStr),
		zap.String("to_format", toStr),
		zap.String("final_format", string(currentPayload.Format())),
	)

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
		return fmt.Errorf("error transcoding payload for unmarshaling: %w", err)
	}

	unmarshaler, ok = t.unmarshalers[path.Nodes[len(path.Nodes)-1]]
	if !ok {
		return fmt.Errorf(
			"unmarshaler not found for format %s, even though a path was found",
			path.Nodes[len(path.Nodes)-1],
		)
	}

	return unmarshaler.Unmarshal(transcodedPayload, v)
}
