// SPDX-License-Identifier: MPL-2.0

package text

import "strings"

// Wrap breaks each line of s at spaces so no line exceeds limit cells. Words
// wider than limit are broken at grapheme boundaries. SGR state is closed at
// every inserted break and reopened on the next line. A limit below one
// returns s unchanged.
func Wrap(s string, limit int) string {
	if limit < 1 || Width(s) <= limit && !strings.Contains(s, "\n") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = wrapLine(line, limit)
	}
	return strings.Join(lines, "\n")
}

type wrapper struct {
	out     strings.Builder
	limit   int
	lineW   int // width on the current output line
	word    strings.Builder
	wordW   int
	spaces  strings.Builder
	spacesW int
	pen     Style // SGR state after everything scanned so far
	emitted Style // SGR state at the end of out
}

func wrapLine(line string, limit int) string {
	w := &wrapper{limit: limit}
	for len(line) > 0 {
		t := Next(line)
		line = line[len(t.Text):]
		switch t.Kind {
		case Cluster:
			if t.Text == " " {
				w.flushWord()
				w.spaces.WriteString(t.Text)
				w.spacesW++
				continue
			}
			if w.wordW+t.Width > limit {
				w.flushWord()
				w.breakLine()
			}
			w.word.WriteString(t.Text)
			w.wordW += t.Width
		case Control:
			w.word.WriteString(t.Text)
		default:
			if body, ok := t.SGR(); ok {
				w.pen.ApplySGR(body)
			}
			w.word.WriteString(t.Text)
		}
	}
	w.flushWord()
	return w.out.String()
}

func (w *wrapper) flushWord() {
	if w.word.Len() == 0 {
		return
	}
	if w.lineW > 0 && w.lineW+w.spacesW+w.wordW > w.limit {
		w.breakLine()
	}
	if w.lineW > 0 {
		w.out.WriteString(w.spaces.String())
		w.lineW += w.spacesW
	}
	w.spaces.Reset()
	w.spacesW = 0
	w.out.WriteString(w.word.String())
	w.lineW += w.wordW
	w.word.Reset()
	w.wordW = 0
	w.emitted = w.pen
}

// breakLine ends the current output line. Pending spaces are dropped and the
// pen state is carried across the break.
func (w *wrapper) breakLine() {
	if w.lineW == 0 {
		return
	}
	w.spaces.Reset()
	w.spacesW = 0
	if !w.emitted.IsZero() {
		w.out.WriteString(ResetStyle)
	}
	w.out.WriteByte('\n')
	if !w.emitted.IsZero() {
		w.out.WriteString(w.emitted.String())
	}
	w.lineW = 0
}
