package safe

import "testing"

func TestText(t *testing.T) {
	cases := []struct{ in, want string }{
		// Plain and UTF-8 text pass through untouched. The em-dash
		// encodes bytes 0xE2 0x80 0x94 - rune scanning must not
		// mistake the 0x80 continuation for a C1 control.
		{"hello, world", "hello, world"},
		{"héllo — ⏎ → ok", "héllo — ⏎ → ok"},

		// CSI: colors, cursor moves, erase-screen, keyboard remap.
		{"\x1b[31mred\x1b[0m", "red"},
		{"a\x1b[2Jb\x1b[Hc", "abc"},
		{"\x1b[?1h\x1b[?1l", ""},
		{"\x1b[38;5;196mX\x1b[m", "X"},
		{"row1\x1b[Grow2", "row1row2"}, // CSI G: cursor move stripped

		// OSC: window-title injection, BEL- and ST-terminated.
		{"\x1b]0;pwned\x07after", "after"},
		{"\x1b]2;title\x1b\\after", "after"},

		// DCS and friends, ST-terminated.
		{"\x1bP+q544e\x1b\\x", "x"},
		{"\x1b_apc\x1b\\ok", "ok"},

		// Intermediate (nF) and two-rune (Fe) forms.
		{"\x1b(Bx", "x"},
		{"a\x1bcb", "ab"},

		// C0: everything except \n and \t is dropped; CR tricks die.
		{"a\x00b\x07c", "abc"},
		{"a\rb\r\nb", "ab\nb"},
		{"a\tb\nc", "a\tb\nc"},

		// DEL and C1 runes (8-bit controls) are dropped.
		{"a\x7fb", "ab"},
		{"a\bb", "ab"},
		{"a\u009bb", "ab"}, // U+009B: 8-bit CSI
		{"a\u0085b", "ab"}, // U+0085: NEL
		// U+009B is dropped; the leftover "[31m" is inert text.
		{"a\u009b[31mb", "a[31mb"},

		// Unterminated sequences never leak bytes.
		{"\x1b[31", ""},
		{"\x1b", ""},
		{"\x1b]0;oops", ""},
		{"before\x1b]0;oops\nafter", "before\nafter"}, // OSC ends at newline
		{"a\x1b]0;t\x1b7b", "ab"},                     // ESC not starting ST ends the OSC
	}
	for _, c := range cases {
		if got := Text(c.in); got != c.want {
			t.Errorf("Text(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTextIdempotent(t *testing.T) {
	once := Text("a\x1b]0;t\x07\x1b[2Jb")
	if again := Text(once); again != once {
		t.Errorf("Text not idempotent: %q then %q", once, again)
	}
}

func TestTextLargeUntrustedPayload(t *testing.T) {
	// A pathological unterminated OSC must not hang or blow up.
	in := make([]rune, 1<<16)
	for i := range in {
		in[i] = 'x'
	}
	in[0] = 0x1b
	in[1] = ']'
	got := Text(string(in))
	if got != "" {
		t.Errorf("unterminated OSC payload leaked %d runes", len(got))
	}
}
