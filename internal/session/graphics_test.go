package session

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func TestKittyForwardable(t *testing.T) {
	for in, want := range map[string]bool{
		"Ga=t,f=100,i=1;AAAA":   true,  // upload
		"Gm=1;AAAA":             true,  // later chunk of an upload
		"Ga=d,d=i,i=1":          true,  // delete
		"Ga=p,U=1,i=1,c=4,r=2":  true,  // virtual placement
		"Ga=T,U=1,i=1,f=100;AA": true,  // upload and virtual placement
		"Ga=p,i=1":              false, // placement at the cursor
		"Ga=T,f=100;AAAA":       false, // upload and place at the cursor
	} {
		if got := kittyForwardable([]byte(in)); got != want {
			t.Errorf("kittyForwardable(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestGraphicsPassThrough(t *testing.T) {
	var forwarded [][]byte
	EnableGraphics("kitty(0.42.1)", func(b []byte) { forwarded = append(forwarded, b) })
	t.Cleanup(func() { EnableGraphics("", nil) })

	emu := vt.NewSafeEmulator(20, 5)
	installGraphics(emu, emu.InputPipe())
	replies := make(chan string, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := io.ReadAtLeast(emu, buf, 1)
		replies <- string(buf[:n])
	}()

	_, _ = emu.Write([]byte("\x1b[>q"))
	select {
	case got := <-replies:
		if got != "\x1bP>|kitty(0.42.1)\x1b\\" {
			t.Fatalf("XTVERSION reply = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no XTVERSION reply")
	}

	_, _ = emu.Write([]byte("\x1b_Ga=T,U=1,i=7,f=100;AAAA\x1b\\\x1b_Ga=p,i=7\x1b\\"))
	if len(forwarded) != 1 || !bytes.Equal(forwarded[0], []byte("\x1b_Ga=T,U=1,i=7,f=100;AAAA\x1b\\")) {
		t.Fatalf("forwarded %q", forwarded)
	}

	// The placeholder cell a virtual placement is drawn on survives the
	// emulator: the character, its diacritics, and the colour that names the
	// image.
	_, _ = emu.Write([]byte("\x1b[38;5;7m\U0010EEEE̅̅\x1b[0m"))
	if r := emu.Render(); !strings.Contains(r, "\U0010EEEE̅̅") {
		t.Fatalf("placeholder lost in render: %q", r)
	}
}

func TestWinsizeCarriesPixels(t *testing.T) {
	cellW.Store(9)
	cellH.Store(18)
	t.Cleanup(func() { cellW.Store(0); cellH.Store(0) })
	ws := winsize(80, 24)
	if ws.X != 720 || ws.Y != 432 || ws.Cols != 80 || ws.Rows != 24 {
		t.Fatalf("winsize = %+v", ws)
	}
}
