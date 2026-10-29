package pdf

// Glyph widths (per 1000 units of font size) for WinAnsi codes 32..126 from
// the Adobe Helvetica and Helvetica-Bold AFM metrics. Oblique shares the
// regular widths.
var helvetica = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, // space - /
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, // 0 - ?
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, // @ - O
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, // P - _
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, // ` - o
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584, // p - ~
}

var helveticaBold = [95]int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
}

// winAnsiExtra maps the non-Latin-1 characters of WinAnsiEncoding (0x80-0x9F).
var winAnsiExtra = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87,
	'ˆ': 0x88, '‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E,
	'‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97,
	'˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

// Widths of selected high-range glyphs; others default to the width of 'n'.
var extraWidth = map[byte][2]int{ // [regular, bold]
	0x85: {1000, 1000}, 0x91: {222, 278}, 0x92: {222, 278}, 0x93: {333, 500}, 0x94: {333, 500},
	0x95: {350, 350}, 0x96: {556, 556}, 0x97: {1000, 1000}, 0x99: {1000, 1000}, 0xA0: {278, 278},
	0xB7: {278, 278}, 0xA9: {737, 737}, 0xAE: {737, 737},
}

// encode converts a UTF-8 string to WinAnsi bytes.
func encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			out = append(out, ' ')
		case r >= 32 && r < 127:
			out = append(out, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			if b, ok := winAnsiExtra[r]; ok {
				out = append(out, b)
			} else if r >= 32 {
				out = append(out, '?')
			}
		}
	}
	return out
}

// Width returns the rendered width of s in points.
func Width(s string, f Font, size float64) float64 {
	total := 0
	for _, c := range encode(s) {
		total += glyphWidth(c, f)
	}
	return float64(total) * size / 1000
}

func glyphWidth(c byte, f Font) int {
	bold := f == Bold
	if c >= 32 && c <= 126 {
		if bold {
			return helveticaBold[c-32]
		}
		return helvetica[c-32]
	}
	if w, ok := extraWidth[c]; ok {
		if bold {
			return w[1]
		}
		return w[0]
	}
	if bold {
		return 611
	}
	return 556
}
