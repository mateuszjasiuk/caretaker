package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
)

// A kitty image placeholder cell must reach the real terminal intact: the
// character, its row/column diacritics, and the foreground colour that names
// the image. This runs one through the same renderer Bubble Tea uses.
func TestPlaceholderSurvivesTerminalRenderer(t *testing.T) {
	cell := "\U0010EEEE̅̍"
	scr := uv.NewScreenBuffer(10, 1)
	uv.NewStyledString("\x1b[38;2;0;0;7m"+cell+"\x1b[0m").Draw(scr, scr.Bounds())

	var out bytes.Buffer
	r := uv.NewTerminalRenderer(&out, []string{"TERM=xterm-kitty"})
	r.SetColorProfile(colorprofile.TrueColor) // what Bubble Tea detects on kitty's tty
	r.Render(scr.RenderBuffer)
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, cell) {
		t.Fatalf("placeholder and diacritics lost: %q", got)
	}
	if !strings.Contains(got, "38;2;0;0;7") {
		t.Fatalf("image colour lost: %q", got)
	}
}
