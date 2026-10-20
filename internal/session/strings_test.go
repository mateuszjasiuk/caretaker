package session

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestStringGuard(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain ✳ text", "plain ✳ text"},                                     // outside strings: untouched
		{"\x1b]0;✳ proud-quartz\x1b\\x", "\x1b]0;� proud-quartz\x1b\\x"},     // 0x9C inside a title
		{"\x1b]0;· name\x07✳", "\x1b]0;· name\x07✳"},                         // safe glyph kept; after BEL is text again
		{"\x1b]8;;https://ex.com/é\x1b\\", "\x1b]8;;https://ex.com/é\x1b\\"}, // safe non-ASCII kept
		{"\x1b[31m✳\x1b[0m", "\x1b[31m✳\x1b[0m"},                             // CSI is not a string
		{"\x1b_Ga=t;AAAA\x1b\\", "\x1b_Ga=t;AAAA\x1b\\"},                     // APC passes
	} {
		var g stringGuard
		if got := string(g.filter([]byte(tc.in))); got != tc.want {
			t.Errorf("filter(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringGuardAcrossWrites(t *testing.T) {
	in := []byte("\x1b]0;✳ proud-quartz\x1b\\")
	for split := 1; split < len(in); split++ {
		var g stringGuard
		got := string(g.filter(in[:split])) + string(g.filter(in[split:]))
		if got != "\x1b]0;� proud-quartz\x1b\\" {
			t.Fatalf("split at %d: %q", split, got)
		}
	}
}

// The bug itself: a title with "✳" must not print the session name on screen.
func TestStringGuardKeepsTitleOffScreen(t *testing.T) {
	var g stringGuard
	e := vt.NewSafeEmulator(40, 3)
	_, _ = e.Write(g.filter([]byte("\x1b[2;3H\x1b]0;✳ proud-quartz\x1b\\")))
	if got := strings.TrimSpace(ansi.Strip(e.Render())); got != "" {
		t.Fatalf("title leaked onto the screen: %q", got)
	}
}
