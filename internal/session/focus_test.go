package session

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// focusSession is a Session with an emulator and no program behind it; what
// the program would read arrives on the returned channel.
func focusSession(t *testing.T) (*Session, <-chan string) {
	t.Helper()
	s := &Session{emu: vt.NewSafeEmulator(10, 2)}
	s.installFocus(s.emu)
	out := make(chan string, 8)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := s.emu.Read(buf)
			if err != nil {
				return
			}
			out <- string(buf[:n])
		}
	}()
	t.Cleanup(func() { _ = s.emu.Close() })
	return s, out
}

func expectRead(t *testing.T, out <-chan string, want string) {
	t.Helper()
	select {
	case got := <-out:
		if got != want {
			t.Fatalf("program read %q, want %q", got, want)
		}
	case <-time.After(200 * time.Millisecond):
		if want != "" {
			t.Fatalf("program read nothing, want %q", want)
		}
	}
}

func TestSetFocusedOnlyWhenReportingIsOn(t *testing.T) {
	s, out := focusSession(t)
	s.SetFocused(true)
	expectRead(t, out, "") // mode 1004 is off: nothing is sent

	_, _ = s.emu.Write([]byte("\x1b[?1004h"))
	s.SetFocused(false)
	expectRead(t, out, "\x1b[O")
	s.SetFocused(true)
	expectRead(t, out, "\x1b[I")

	_, _ = s.emu.Write([]byte("\x1b[?1004l"))
	s.SetFocused(false)
	expectRead(t, out, "")
}
