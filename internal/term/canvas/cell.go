// SPDX-License-Identifier: MPL-2.0

package canvas

import "github.com/wippyai/runtime/internal/term/text"

// Link is an OSC 8 hyperlink attached to a cell.
type Link struct {
	Params string
	URL    string
}

// IsZero reports whether l is no link.
func (l Link) IsZero() bool { return l == Link{} }

// Cell is one terminal cell. A cell of Width 2 or more is followed by
// zero-value placeholder cells covering the columns it spans.
type Cell struct {
	// Content is one grapheme cluster.
	Content string
	Style   text.Style
	Link    Link
	Width   int
}

// EmptyCell is an unstyled blank.
var EmptyCell = Cell{Content: " ", Width: 1}

// IsZero reports whether c is a wide-cell placeholder.
func (c Cell) IsZero() bool { return c == Cell{} }

// Blank returns a space cell keeping c's style and link.
func (c Cell) Blank() Cell {
	c.Content, c.Width = " ", 1
	return c
}
