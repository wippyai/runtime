// SPDX-License-Identifier: MPL-2.0

package tty

const TopicEvents = "@tty/events"

// Event is a transport-neutral terminal event. Physical ports produce input
// and host-state events; compositors may additionally send lifecycle events to
// virtual ports. Fields not associated with Type are ignored.
type Event struct {
	Type    string
	Key     string
	KeyType string
	Action  string
	Button  string
	Paste   string
	// X and Y are one-based terminal cell coordinates. Nested compositors
	// translate them into the child's one-based viewport space.
	X      int
	Y      int
	Width  int
	Height int
	Alt    bool
	Ctrl   bool
	Shift  bool
	// Focused reports keyboard-input ownership. It does not imply visibility.
	Focused bool
	// Visible reports whether publishing presentation updates is useful. It
	// does not imply focus or prescribe whether the producer keeps computing.
	Visible bool
}
