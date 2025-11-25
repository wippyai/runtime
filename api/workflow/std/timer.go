package std

// TimerHeader is the header payload for timer.sleep commands.
// This is serialized as Params[0]. Timers typically have no additional arguments.
type TimerHeader struct {
	// Milliseconds is the timer duration in milliseconds.
	Milliseconds int64 `json:"ms"`

	// Summary is an optional name/description for debugging.
	// Visible in Temporal UI and CLI.
	Summary string `json:"summary,omitempty"`
}
