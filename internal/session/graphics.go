package session

import (
	"bytes"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Programs inside ct draw on ct's own emulator, not on the real terminal, so
// kitty's image protocol needs three things passed through:
//
//   - the real terminal's name, so a program asking "which terminal is this?"
//     (XTVERSION) learns it may send images;
//   - the cell size in pixels, which programs read from the pty size to scale
//     images;
//   - the image commands themselves, forwarded to the real terminal.
//
// Only commands that do not depend on the cursor are forwarded: uploads,
// deletes, and "virtual" placements (U=1). A virtual placement is shown by
// kitty wherever its Unicode placeholder characters appear on screen, and
// those are plain text that ct's emulator copies like any other. A direct
// placement would land wherever ct's own cursor happens to be, so it is
// dropped.

var graphics struct {
	mu      sync.Mutex
	version string       // the real terminal's XTVERSION name, e.g. "kitty(0.42.1)"
	forward func([]byte) // writes a sequence to the real terminal; nil = off
}

// cell size in pixels of the real terminal; 0 when unknown.
var cellW, cellH atomic.Uint32

// EnableGraphics turns on image pass-through for sessions: version is the
// real terminal's XTVERSION name, and forward writes a raw sequence to it.
func EnableGraphics(version string, forward func([]byte)) {
	graphics.mu.Lock()
	defer graphics.mu.Unlock()
	graphics.version, graphics.forward = version, forward
}

func graphicsConfig() (string, func([]byte)) {
	graphics.mu.Lock()
	defer graphics.mu.Unlock()
	return graphics.version, graphics.forward
}

// MeasureCells records the real terminal's cell size from tty, whose pty size
// carries the window's pixel size. It is a no-op when tty is not a terminal
// or reports no pixels.
func MeasureCells(tty *os.File) {
	ws, err := pty.GetsizeFull(tty)
	if err != nil || ws.Cols == 0 || ws.Rows == 0 || ws.X == 0 || ws.Y == 0 {
		return
	}
	cellW.Store(uint32(ws.X / ws.Cols))
	cellH.Store(uint32(ws.Y / ws.Rows))
}

// winsize is the pty size for a w×h session, in cells and, when known, pixels.
func winsize(w, h int) *pty.Winsize {
	return &pty.Winsize{
		Rows: uint16(h), Cols: uint16(w),
		X: uint16(uint32(w) * cellW.Load()), Y: uint16(uint32(h) * cellH.Load()),
	}
}

// installGraphics hooks the image pass-through into emu. reply writes back to
// the program, as a terminal answering a query would.
func installGraphics(emu *vt.SafeEmulator, reply io.Writer) {
	// XTVERSION: answer with the real terminal's name when known.
	emu.RegisterCsiHandler(ansi.Command('>', 0, 'q'), func(ansi.Params) bool {
		version, _ := graphicsConfig()
		if version == "" {
			return false
		}
		_, _ = io.WriteString(reply, "\x1bP>|"+version+"\x1b\\")
		return true
	})
	emu.RegisterApcHandler(func(data []byte) bool {
		if len(data) == 0 || data[0] != 'G' {
			return false
		}
		if _, forward := graphicsConfig(); forward != nil && kittyForwardable(data) {
			forward(append([]byte("\x1b_"), append(append([]byte(nil), data...), '\x1b', '\\')...))
		}
		return true
	})
}

// kittyForwardable reports whether a kitty graphics command (the APC payload,
// "G" + keys + optional ";data") is safe to pass to the real terminal: anything
// but a placement that is not virtual.
func kittyForwardable(data []byte) bool {
	keys := data[1:]
	if i := bytes.IndexByte(keys, ';'); i >= 0 {
		keys = keys[:i]
	}
	action, virtual := "t", false
	for kv := range bytes.SplitSeq(keys, []byte{','}) {
		k, v, _ := bytes.Cut(kv, []byte{'='})
		switch string(k) {
		case "a":
			action = string(v)
		case "U":
			virtual = string(v) == "1"
		}
	}
	return (action != "T" && action != "p") || virtual
}
