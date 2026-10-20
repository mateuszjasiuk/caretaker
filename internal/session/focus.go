package session

import (
	"io"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// Focus reporting: a program that sends CSI ? 1004 h wants CSI I when its
// session gains focus and CSI O when it loses it (Neovim's FocusGained and
// FocusLost). ct tracks the mode itself, from the program's output, because
// the emulator keeps it in a map that is not safe to read while output is
// being written.

// installFocus records whether the program has turned focus reporting on.
// The handlers only observe: they return false so the emulator still applies
// the mode change itself.
func (s *Session) installFocus(emu *vt.SafeEmulator) {
	watch := func(on bool) func(ansi.Params) bool {
		return func(params ansi.Params) bool {
			params.ForEach(-1, func(_, mode int, _ bool) {
				if mode == int(ansi.ModeFocusEvent) {
					s.reportFocus.Store(on)
				}
			})
			return false
		}
	}
	emu.RegisterCsiHandler(ansi.Command('?', 0, 'h'), watch(true))
	emu.RegisterCsiHandler(ansi.Command('?', 0, 'l'), watch(false))
}

// SetFocused tells the program it gained or lost focus, when it asked to
// know. A program that has not turned on focus reporting gets nothing.
func (s *Session) SetFocused(focused bool) {
	if !s.reportFocus.Load() || !s.Alive() {
		return
	}
	seq := ansi.Blur
	if focused {
		seq = ansi.Focus
	}
	_, _ = io.WriteString(s.emu.InputPipe(), seq)
}
