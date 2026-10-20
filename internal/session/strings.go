package session

import "unicode/utf8"

// stringGuard protects the emulator's parser from a misreading of control
// strings: window titles (OSC), DCS, APC, PM and SOS. Inside them the parser
// treats any byte from 0x80 to 0x9F as an 8-bit control, such as 0x9C (ST,
// "end of string"), even when that byte is part of a UTF-8 character. Claude
// Code puts spinner glyphs like "✳" (e2 9c b3) in its window title, so the
// title ended early and its rest (the session name) was printed at the
// cursor, in the middle of the input box.
//
// The guard replaces such a character inside a string with U+FFFD, whose
// encoding has no byte in that range. Everything else passes through as is.
// It keeps state across writes, since a sequence can be split between reads.
type stringGuard struct {
	mode    guardMode
	pending []byte // a UTF-8 character inside a string, not yet complete
	need    int    // its full length in bytes
}

type guardMode uint8

const (
	guardText      guardMode = iota // ordinary output
	guardEsc                        // just after ESC
	guardString                     // inside a control string
	guardStringEsc                  // just after ESC inside a control string
)

// filter returns p with unsafe characters inside control strings replaced.
// The result may hold back the start of a character split across writes.
func (g *stringGuard) filter(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for _, b := range p {
		switch g.mode {
		case guardText:
			if b == 0x1b {
				g.mode = guardEsc
			}
			out = append(out, b)
		case guardEsc:
			g.mode = g.afterEsc(b)
			out = append(out, b)
		case guardString:
			out = g.stringByte(out, b)
		case guardStringEsc:
			// "ESC \" ends the string; any other ESC sequence also ends it
			// and starts a new one.
			if b == '\\' {
				g.mode = guardText
			} else {
				g.mode = g.afterEsc(b)
			}
			out = append(out, b)
		}
	}
	return out
}

// afterEsc is the mode after ESC b.
func (g *stringGuard) afterEsc(b byte) guardMode {
	switch b {
	case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS
		return guardString
	case 0x1b:
		return guardEsc
	}
	return guardText
}

// stringByte handles one byte inside a control string.
func (g *stringGuard) stringByte(out []byte, b byte) []byte {
	if len(g.pending) > 0 {
		if b&0xC0 == 0x80 { // continuation byte
			g.pending = append(g.pending, b)
			if len(g.pending) < g.need {
				return out
			}
			out = appendSafe(out, g.pending)
			g.pending = g.pending[:0]
			return out
		}
		// A broken character: drop it, then handle b on its own.
		out = append(out, "�"...)
		g.pending = g.pending[:0]
	}
	switch {
	case b == 0x07: // BEL ends the string
		g.mode = guardText
	case b == 0x1b:
		g.mode = guardStringEsc
	case b >= 0xC2 && b <= 0xF4: // first byte of a multi-byte character
		g.pending = append(g.pending, b)
		g.need = utf8LenFromLead(b)
		return out
	}
	return append(out, b)
}

// appendSafe appends the UTF-8 character r, or U+FFFD when one of its bytes
// would read as an 8-bit control.
func appendSafe(out, r []byte) []byte {
	for _, b := range r {
		if b >= 0x80 && b <= 0x9F {
			return append(out, "�"...)
		}
	}
	if !utf8.Valid(r) {
		return append(out, "�"...)
	}
	return append(out, r...)
}

func utf8LenFromLead(b byte) int {
	switch {
	case b >= 0xF0:
		return 4
	case b >= 0xE0:
		return 3
	}
	return 2
}
