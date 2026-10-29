// Package pdf renders resumes to PDF using only the standard library.
//
// It writes PDF 1.4 directly with the standard Type 1 fonts (Helvetica
// family), which every PDF reader ships, so no font files or external tools
// are needed. Text is WinAnsi encoded; characters outside that set are
// replaced with '?'.
package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"strings"
	"time"
)

// Font identifies one of the embedded standard fonts.
type Font int

const (
	Regular Font = iota
	Bold
	Italic
)

var fontNames = [...]string{"Helvetica", "Helvetica-Bold", "Helvetica-Oblique"}

// RGB is a color with components in [0,1].
type RGB struct{ R, G, B float64 }

// Paper sizes in points.
var (
	A4     = [2]float64{595.28, 841.89}
	Letter = [2]float64{612, 792}
)

type link struct {
	x, y, w, h float64
	uri        string
}

type page struct {
	content bytes.Buffer
	links   []link
}

// Document is an in-memory PDF under construction.
type Document struct {
	W, H  float64
	Title string
	pages []*page
}

// New creates an empty document with the given page size.
func New(size [2]float64) *Document { return &Document{W: size[0], H: size[1]} }

// AddPage appends a page and makes it current.
func (d *Document) AddPage() { d.pages = append(d.pages, &page{}) }

// PageCount returns the number of pages.
func (d *Document) PageCount() int { return len(d.pages) }

func (d *Document) cur() *page {
	if len(d.pages) == 0 {
		d.AddPage()
	}
	return d.pages[len(d.pages)-1]
}

// Text draws s with its baseline at (x, y), measured from the bottom-left.
func (d *Document) Text(x, y float64, f Font, size float64, c RGB, s string) {
	fmt.Fprintf(&d.cur().content, "BT /F%d %.2f Tf %.3f %.3f %.3f rg %.2f %.2f Td (%s) Tj ET\n",
		int(f)+1, size, c.R, c.G, c.B, x, y, escape(encode(s)))
}

// Line draws a straight line.
func (d *Document) Line(x1, y1, x2, y2, width float64, c RGB) {
	fmt.Fprintf(&d.cur().content, "%.3f %.3f %.3f RG %.2f w %.2f %.2f m %.2f %.2f l S\n",
		c.R, c.G, c.B, width, x1, y1, x2, y2)
}

// Rect fills a rectangle.
func (d *Document) Rect(x, y, w, h float64, c RGB) {
	fmt.Fprintf(&d.cur().content, "%.3f %.3f %.3f rg %.2f %.2f %.2f %.2f re f\n", c.R, c.G, c.B, x, y, w, h)
}

// Link adds a clickable URI annotation over the given rectangle.
func (d *Document) Link(x, y, w, h float64, uri string) {
	p := d.cur()
	p.links = append(p.links, link{x, y, w, h, uri})
}

// WriteTo serializes the document.
func (d *Document) WriteTo(w io.Writer) (int64, error) {
	if len(d.pages) == 0 {
		d.AddPage()
	}
	var buf bytes.Buffer
	var offsets []int
	obj := func(body string) int {
		offsets = append(offsets, buf.Len())
		n := len(offsets)
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}
	buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	// Fixed object numbers: 1 catalog, 2 pages, 3-5 fonts, 6 info.
	nPages := len(d.pages)
	firstPage := 7
	kids := make([]string, nPages)
	// Each page uses: page obj, content obj, then one obj per link.
	next := firstPage
	pageNums := make([]int, nPages)
	for i, p := range d.pages {
		pageNums[i] = next
		kids[i] = fmt.Sprintf("%d 0 R", next)
		next += 2 + len(p.links)
	}

	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), nPages))
	for _, name := range fontNames {
		obj(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", name))
	}
	obj(fmt.Sprintf("<< /Title (%s) /Producer (resume-generator) /CreationDate (D:%s) >>",
		escape(encode(d.Title)), time.Now().UTC().Format("20060102150405Z")))

	for i, p := range d.pages {
		var annots []string
		for j := range p.links {
			annots = append(annots, fmt.Sprintf("%d 0 R", pageNums[i]+2+j))
		}
		annotStr := ""
		if len(annots) > 0 {
			annotStr = " /Annots [" + strings.Join(annots, " ") + "]"
		}
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] "+
			"/Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R >> >> /Contents %d 0 R%s >>",
			d.W, d.H, pageNums[i]+1, annotStr))

		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		zw.Write(p.content.Bytes())
		zw.Close()
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", len(offsets), z.Len())
		buf.Write(z.Bytes())
		buf.WriteString("\nendstream\nendobj\n")

		for _, l := range p.links {
			obj(fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [%.2f %.2f %.2f %.2f] /Border [0 0 0] "+
				"/A << /S /URI /URI (%s) >> >>", l.x, l.y, l.x+l.w, l.y+l.h, escape([]byte(asciiOnly(l.uri)))))
		}
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	n, err := w.Write(buf.Bytes())
	return int64(n), err
}

func escape(b []byte) []byte {
	var out bytes.Buffer
	for _, c := range b {
		switch c {
		case '\\', '(', ')':
			out.WriteByte('\\')
			out.WriteByte(c)
		case '\r', '\n':
			out.WriteByte(' ')
		default:
			out.WriteByte(c)
		}
	}
	return out.Bytes()
}

func asciiOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x21 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}
