package stages

type SerializableEntry struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Meta   map[string]any `json:"meta,omitempty"`
	Data   any            `json:"data"`
	Format string         `json:"format,omitempty"`
}

type SerializableState struct {
	Entries []SerializableEntry `json:"entries"`
}
