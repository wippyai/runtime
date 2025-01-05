package __redo

type Pattern struct {
	Kind string // Match on Kind
	From ID     // Optional sender match
}

func matchMessage(pattern Pattern, msg Message) bool {
	// Match Kind if specified
	if pattern.Kind != "" && pattern.Kind != msg.Kind {
		return false
	}

	// Match From if specified
	if pattern.From != "" && pattern.From != msg.From {
		return false
	}

	return true
}
