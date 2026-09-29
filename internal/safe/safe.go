// Package safe strips terminal control sequences from untrusted text
// before it reaches the display. Tool results (file contents, shell
// output, fetched pages) and model output are data, not instructions:
// none of it may retitle the window, clear the screen, remap keys, or
// otherwise drive the terminal.
package safe

import "strings"

// Text returns s with escape sequences and control characters removed.
// Newlines and tabs survive; every other C0/C1 control, DEL, and all
// ESC-initiated sequences (CSI, OSC, DCS, SOS, PM, APC, and the
// intermediate and two-rune forms) are dropped.
//
// Scanning is by rune: UTF-8 text passes through untouched (a rune
// like em-dash encodes bytes 0x80–0x9F as continuations, which byte
// scanning would corrupt), and invalid bytes become U+FFFD, inert
// display text.
//
// An unterminated sequence is consumed, never passed on — the failure
// direction is display fidelity, never terminal control.
func Text(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == 0x1b:
			i = skipSequence(rs, i)
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// C0 minus the keep-set, DEL, and C1: dropped. Some
			// terminals execute 8-bit C1 (0x9B acts as CSI).
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// skipSequence consumes the escape sequence starting at rs[i] (which
// is ESC) and returns the index of its last consumed rune.
func skipSequence(rs []rune, i int) int {
	if i+1 >= len(rs) {
		return i // lone trailing ESC
	}
	switch c := rs[i+1]; {
	case c == '[':
		// CSI: parameters 0x30–0x3F and intermediates 0x20–0x2F,
		// then one final byte 0x40–0x7E.
		j := i + 2
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x3f {
			j++
		}
		if j < len(rs) && rs[j] >= 0x40 && rs[j] <= 0x7e {
			return j
		}
		return j - 1 // unterminated: the scan itself is discarded
	case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
		// OSC / DCS / SOS / PM / APC: a string terminated by BEL
		// or ST (ESC \). A stray ESC that does not start ST ends
		// the sequence instead of swallowing the rest of the file.
		j := i + 2
		for j < len(rs) {
			switch rs[j] {
			case 0x07:
				return j
			case '\n':
				// A title with a newline in it is not a title; an
				// unterminated OSC must not swallow the rest of
				// the file. End the sequence, keep the newline.
				return j - 1
			case 0x1b:
				if j+1 < len(rs) && rs[j+1] == '\\' {
					return j + 1
				}
				return j - 1
			}
			j++
		}
		return len(rs) - 1
	case c >= 0x20 && c <= 0x2f:
		// nF: zero or more intermediates (0x20–0x2F) then one
		// final byte 0x30–0x7E. Covers ESC (B and friends.
		j := i + 1
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f {
			j++
		}
		if j < len(rs) && rs[j] >= 0x30 && rs[j] <= 0x7e {
			return j
		}
		return j - 1
	default:
		// Fe/Fs two-rune sequences (ESC c, ESC 7, ESC M …).
		return i + 1
	}
}
