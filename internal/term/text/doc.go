// SPDX-License-Identifier: MPL-2.0

// Package text measures, cuts, wraps and styles terminal text. Strings may
// carry SGR styling, OSC 8 hyperlinks and other escape sequences; widths are
// counted in terminal cells over grapheme clusters, so wide CJK, emoji ZWJ
// sequences and combining marks measure as a terminal renders them.
package text
