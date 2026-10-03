// SPDX-License-Identifier: MPL-2.0

package text

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoxZeroValueIsPassThrough(t *testing.T) {
	require.Equal(t, "a    b\nlonger", Box{}.Render("a\tb\nlonger"))
	require.Equal(t, "hello world", Box{}.Render("hello", "world"))
}

func TestBoxStyleSequences(t *testing.T) {
	require.Equal(t, "\x1b[1;38;2;255;0;0mhi\x1b[m", Box{}.Foreground(RGB(255, 0, 0)).Bold(true).Render("hi"))
	require.Equal(t, "\x1b[4;44mhi\x1b[m", Box{}.Background(Basic(4)).Underline(true).Render("hi"))
	require.Equal(t, "\x1b[1mhi\x1b[m", Box{}.Bold(true).Render("hi"))
	require.Equal(t, "hi", Box{}.Bold(true).Bold(false).Render("hi"))
	require.Equal(t, "\x1b[2;3;5;7;9mhi\x1b[m",
		Box{}.Faint(true).Italic(true).Blink(true).Reverse(true).Strikethrough(true).Render("hi"))
}

func TestBoxIsAValue(t *testing.T) {
	base := Box{}.Foreground(Basic(1))
	derived := base.Width(10)
	require.NotEqual(t, base.Render("x"), derived.Render("x"))
	require.Equal(t, Box{}.Foreground(Basic(1)).Render("y"), base.Render("y"))
}

func TestBoxPaddingWidthAndAlignment(t *testing.T) {
	require.Equal(t, "  ab  ", Box{}.Padding(0, 2).Render("ab"))
	require.Equal(t, "    \n ab \n    ", Box{}.Padding(1, 1).Render("ab"))
	require.Equal(t, "    ab", Box{}.Width(6).Align(Right).Render("ab"))
	require.Equal(t, "  ab  ", Box{}.Width(6).Align(Center).Render("ab"))
	require.Equal(t, " 世界 ", Box{}.Width(6).Align(Center).Render("世界"))
	require.Equal(t, "hello\nworld", Box{}.Width(5).Render("hello world"))
	require.Equal(t, " hi  \n yo  ", Box{}.Padding(0, 0, 0, 1).Width(5).Render("hi yo"))
}

func TestBoxVerticalAlignment(t *testing.T) {
	require.Equal(t, " \n \nx", Box{}.Height(3).AlignVertical(Bottom).Render("x"))
	require.Equal(t, "x\n \n ", Box{}.Height(3).AlignVertical(Top).Render("x"))
	require.Equal(t, " \nx\n ", Box{}.Height(3).AlignVertical(Center).Render("x"))
}

func TestBoxBorders(t *testing.T) {
	require.Equal(t, "┌──┐\n│ab│\n└──┘", Box{}.Border(NormalBorder).Render("ab"))
	require.Equal(t, "╭──╮\n│ab│\n╰──╯", Box{}.Border(RoundedBorder).Render("ab"))
	require.Equal(t, "┏━━┓\n┃ab┃\n┗━━┛", Box{}.Border(ThickBorder).Render("ab"))
	require.Equal(t, "╔══╗\n║ab║\n╚══╝", Box{}.Border(DoubleBorder).Render("ab"))
	require.Equal(t, "    \n ab \n    ", Box{}.Border(HiddenBorder).Render("ab"))
	require.Equal(t, "──\nab\n──", Box{}.Border(NormalBorder, true, false, true, false).Render("ab"))
	require.Equal(t, "┌─\n│a\n└─", Box{}.Border(NormalBorder, true, false, true, true).Render("a"))
	require.Equal(t, "┌──┐\n│世│\n└──┘", Box{}.Border(NormalBorder).Render("世"))
}

func TestBoxBorderColors(t *testing.T) {
	got := Box{}.Border(NormalBorder).BorderForeground(Basic(1)).Render("a")
	require.Equal(t, "\x1b[31m┌─┐\x1b[m\n\x1b[31m│\x1b[ma\x1b[31m│\x1b[m\n\x1b[31m└─┘\x1b[m", got)
	got = Box{}.Border(NormalBorder, true, false, false, false).
		BorderForeground(Basic(1)).BorderBackground(Basic(2)).Render("a")
	require.Equal(t, "\x1b[31;42m─\x1b[m\na", got)
}

func TestBoxMarginAndLimits(t *testing.T) {
	require.Equal(t, "      \n  ab  \n      ", Box{}.Margin(1, 2).Render("ab"))
	require.Equal(t, "abc", Box{}.MaxWidth(3).Render("abcdef"))
	require.Equal(t, "a", Box{}.MaxHeight(1).Render("a\nb"))
	require.Equal(t, "ab", Box{}.Padding(1).Inline(true).Render("a\nb"))
}

func TestBoxReverseStylesWhitespace(t *testing.T) {
	got := Box{}.Padding(0, 1).Reverse(true).Foreground(Basic(1)).Background(Basic(2)).Render("x")
	sp := "\x1b[7;31;42m \x1b[m"
	require.Equal(t, sp+"\x1b[7;31;42mx\x1b[m"+sp, got)
}

func TestBoxPaddingBackground(t *testing.T) {
	got := Box{}.Padding(0, 1).Background(Basic(2)).Render("x")
	require.Equal(t, "\x1b[42m \x1b[m\x1b[42mx\x1b[m\x1b[42m \x1b[m", got)
}

func TestBoxShorthandIgnoresInvalidCounts(t *testing.T) {
	require.Equal(t, "x", Box{}.Padding(1, 2, 3, 4, 5).Render("x"))
	require.Equal(t, "  x", Box{}.Padding(0, 0, 0, 2).Render("x"))
	require.Equal(t, " \n \nx\n ", Box{}.Padding(2, 0, 1).Render("x"))
}
