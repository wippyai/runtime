package style

import "testing"

func force(p profile) {
	profileOnce.Do(func() {})
	current = p
}

func TestRenderNone(t *testing.T) {
	force(profileNone)
	if got := New().Bold(true).Foreground(9).Render("a\tb"); got != "a    b" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderColor(t *testing.T) {
	force(profile256)
	got := New().Bold(true).Foreground(14).Render("x\n\ny")
	want := "\x1b[1;96mx\x1b[0m\n\n\x1b[1;96my\x1b[0m"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := New().Foreground(8).Render(""); got != "" {
		t.Fatalf("empty got %q", got)
	}
	if got := New().Foreground(220).Render("x"); got != "\x1b[38;5;220mx\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestRender16Downgrade(t *testing.T) {
	force(profile16)
	if got := New().Foreground(196).Render("x"); got != "\x1b[91mx\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}
