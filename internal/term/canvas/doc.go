// SPDX-License-Identifier: MPL-2.0

// Package canvas is a styled cell buffer. Styled strings are decoded once into
// cells at the placement boundary, so clipped escape sequences cannot leak into
// rendered rows and overlapping content composes cell by cell.
package canvas
