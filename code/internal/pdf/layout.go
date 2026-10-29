package pdf

import "strings"

// Style describes how a run of text is drawn.
type Style struct {
	Font  Font
	Size  float64
	Color RGB
}

// Run is a piece of styled text, optionally linked.
type Run struct {
	Text  string
	Style Style
	Link  string
}

// Layout is a top-to-bottom flow cursor over a Document with automatic page breaks.
type Layout struct {
	Doc                      *Document
	Left, Right, Top, Bottom float64 // margins in points
	Y                        float64 // current baseline position from the bottom
}

// NewLayout starts a document with one page.
func NewLayout(doc *Document, margin float64) *Layout {
	l := &Layout{Doc: doc, Left: margin, Right: margin, Top: margin, Bottom: margin}
	l.newPage()
	return l
}

// Width is the usable text width.
func (l *Layout) Width() float64 { return l.Doc.W - l.Left - l.Right }

func (l *Layout) newPage() {
	l.Doc.AddPage()
	l.Y = l.Doc.H - l.Top
}

// Ensure starts a new page unless h points of vertical space remain.
func (l *Layout) Ensure(h float64) {
	if l.Y-h < l.Bottom {
		l.newPage()
	}
}

// Space moves the cursor down.
func (l *Layout) Space(h float64) {
	l.Y -= h
	if l.Y < l.Bottom {
		l.newPage()
	}
}

// Rule draws a horizontal line across the text width at the cursor.
func (l *Layout) Rule(width float64, c RGB) {
	l.Doc.Line(l.Left, l.Y, l.Doc.W-l.Right, l.Y, width, c)
}

type token struct {
	text  string
	style Style
	link  string
	space bool // preceded by a space
}

// Paragraph wraps runs to the text width (minus indent and reserve on the
// right) and draws them. If right is non-empty it is drawn right-aligned on
// the first line. bullet, if set, is drawn in the indent of the first line.
func (l *Layout) Paragraph(runs []Run, indent, leading float64, bullet *Run, right *Run) {
	maxW := l.Width() - indent
	firstMax := maxW
	if right != nil && right.Text != "" {
		firstMax -= Width(right.Text, right.Style.Font, right.Style.Size) + 12
	}
	lines := wrap(tokenize(runs), maxW, firstMax)
	if len(lines) == 0 {
		lines = [][]token{nil}
	}
	for i, line := range lines {
		h := leading
		l.Ensure(h)
		l.Y -= h
		baseline := l.Y + h*0.25
		x := l.Left + indent
		if i == 0 && bullet != nil {
			l.Doc.Text(l.Left+indent-Width(bullet.Text, bullet.Style.Font, bullet.Style.Size)-5, baseline,
				bullet.Style.Font, bullet.Style.Size, bullet.Style.Color, bullet.Text)
		}
		// Draw consecutive tokens with the same style and link as one string so
		// text extraction (ATS parsers, copy/paste) sees whole phrases.
		for j := 0; j < len(line); {
			t := line[j]
			if t.space && j > 0 {
				x += Width(" ", t.style.Font, t.style.Size)
			}
			text := t.text
			k := j + 1
			for ; k < len(line) && line[k].style == t.style && line[k].link == t.link; k++ {
				if line[k].space {
					text += " "
				}
				text += line[k].text
			}
			w := Width(text, t.style.Font, t.style.Size)
			l.Doc.Text(x, baseline, t.style.Font, t.style.Size, t.style.Color, text)
			if t.link != "" {
				l.Doc.Link(x, baseline-t.style.Size*0.25, w, t.style.Size*1.1, t.link)
			}
			x += w
			j = k
		}
		if i == 0 && right != nil && right.Text != "" {
			rw := Width(right.Text, right.Style.Font, right.Style.Size)
			l.Doc.Text(l.Doc.W-l.Right-rw, baseline, right.Style.Font, right.Style.Size, right.Style.Color, right.Text)
		}
	}
}

func tokenize(runs []Run) []token {
	var toks []token
	pendingSpace := false
	for _, r := range runs {
		text := strings.ReplaceAll(r.Text, "\n", " ")
		if strings.HasPrefix(text, " ") {
			pendingSpace = true
		}
		words := strings.Fields(text)
		for i, w := range words {
			toks = append(toks, token{text: w, style: r.Style, link: r.Link, space: pendingSpace || i > 0})
			pendingSpace = false
		}
		if strings.HasSuffix(text, " ") {
			pendingSpace = true
		}
	}
	return toks
}

func wrap(toks []token, maxW, firstMax float64) [][]token {
	var lines [][]token
	var line []token
	lineW := 0.0
	limit := firstMax
	for len(toks) > 0 {
		t := toks[0]
		w := Width(t.text, t.style.Font, t.style.Size)
		sp := 0.0
		if len(line) > 0 && t.space {
			sp = Width(" ", t.style.Font, t.style.Size)
		}
		if len(line) > 0 && lineW+sp+w > limit {
			lines = append(lines, line)
			line, lineW, limit = nil, 0, maxW
			continue
		}
		if len(line) == 0 && w > limit {
			// Break a word that is longer than the whole line (e.g. a URL).
			head, tail := splitToWidth(t.text, t.style, limit)
			t.text = head
			toks[0].text = tail
			toks[0].space = false
			lines = append(lines, []token{t})
			limit = maxW
			continue
		}
		line = append(line, t)
		lineW += sp + w
		toks = toks[1:]
	}
	if len(line) > 0 {
		lines = append(lines, line)
	}
	return lines
}

func splitToWidth(s string, st Style, limit float64) (string, string) {
	r := []rune(s)
	n := 1
	for n < len(r) && Width(string(r[:n+1]), st.Font, st.Size) <= limit {
		n++
	}
	return string(r[:n]), string(r[n:])
}
