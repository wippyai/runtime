package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type SerializableEntry struct {
	ID     string                 `json:"id"`
	Kind   string                 `json:"kind"`
	Meta   map[string]interface{} `json:"meta,omitempty"`
	Data   interface{}            `json:"data"`
	Format string                 `json:"format,omitempty"`
}

type SerializableState struct {
	Entries []SerializableEntry `json:"entries"`
}

type loadDumpStage struct {
	dumpFile string
}

func LoadDump(dumpFile string) boot.Stage {
	return &loadDumpStage{dumpFile: dumpFile}
}

func (s *loadDumpStage) Name() string {
	return "load-dump"
}

func (s *loadDumpStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	log := logs.GetLogger(ctx)
	dtt := payload.GetTranscoder(ctx)

	if dtt == nil {
		return fmt.Errorf("transcoder not found in context")
	}

	log.Info("loading state from dump file", zap.String("file", s.dumpFile))

	dumpData, err := os.ReadFile(s.dumpFile)
	if err != nil {
		return fmt.Errorf("read dump file: %w", err)
	}

	var serializable SerializableState
	if err := json.Unmarshal(dumpData, &serializable); err != nil {
		return fmt.Errorf("unmarshal dump file: %w", err)
	}

	log.Info("unmarshaled dump file",
		zap.Int("entries_count", len(serializable.Entries)),
		zap.Int("dump_size", len(dumpData)))

	loadedEntries := make([]registry.Entry, len(serializable.Entries))
	for i, se := range serializable.Entries {
		entry, err := convertFromSerializableEntry(se, dtt)
		if err != nil {
			return fmt.Errorf("convert entry %s: %w", se.ID, err)
		}
		loadedEntries[i] = entry
	}

	*entries = loadedEntries

	log.Info("loaded entries from dump", zap.Int("count", len(loadedEntries)))
	return nil
}

func convertFromSerializableEntry(se SerializableEntry, _ payload.Transcoder) (registry.Entry, error) {
	meta := make(registry.Metadata)
	for k, v := range se.Meta {
		meta[k] = v
	}

	var payloadData payload.Payload
	if se.Data != nil {
		payloadData = payload.New(se.Data)
	}

	entry := registry.Entry{
		ID:   registry.ParseID(se.ID),
		Kind: se.Kind,
		Meta: meta,
		Data: payloadData,
	}

	return entry, nil
}
