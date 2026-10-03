package vt

// SetScrollback changes the number of history lines retained by the normal
// buffer. Lines beyond the new limit are discarded oldest first.
func (t *Terminal) SetScrollback(lines int) {
	lines = min(max(lines, 0), MaxBufferSize)
	t.optionsService.Options.Scrollback = lines
	t.bufferService.Buffers.scrollback = lines
	t.bufferService.Buffers.Normal().setScrollback(lines)
}

func (b *Buffer) setScrollback(lines int) {
	b.scrollback = lines
	maxLength := b.getCorrectBufferLength(b.rows)
	if trim := b.Lines.Length() - maxLength; trim > 0 {
		b.Lines.TrimStart(trim)
		b.YBase = max(b.YBase-trim, 0)
		b.YDisp = max(b.YDisp-trim, 0)
		b.SavedState.Y = max(b.SavedState.Y-trim, 0)
	}
	b.Lines.SetMaxLength(maxLength)
}
