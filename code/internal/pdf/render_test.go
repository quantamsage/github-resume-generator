package pdf

import (
	"bytes"
	"compress/zlib"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/judie-paul/resume-generator/internal/resume"
)

func TestRenderAllTemplates(t *testing.T) {
	r := resume.Sample()
	if err := resume.Sanitize(r); err != nil {
		t.Fatal(err)
	}
	for _, tpl := range Templates {
		for _, paper := range []string{"a4", "letter"} {
			b, err := Render(r, tpl, paper)
			if err != nil {
				t.Fatalf("%s/%s: %v", tpl, paper, err)
			}
			checkStructure(t, b)
			text := allContent(t, b)
			for _, want := range []string{"Alex Developer", "taskqueue", "SKIP LOCKED"} {
				if !strings.Contains(text, want) {
					t.Errorf("%s/%s: content missing %q", tpl, paper, want)
				}
			}
			if !bytes.Contains(b, []byte("/URI (https://github.com/example/taskqueue)")) {
				t.Errorf("%s: project link annotation missing", tpl)
			}
			if dir := os.Getenv("PDF_OUT"); dir != "" {
				os.WriteFile(filepath.Join(dir, tpl+"-"+paper+".pdf"), b, 0o644)
			}
		}
	}
}

func TestPageBreaks(t *testing.T) {
	r := resume.Sample()
	for i := 0; i < 40; i++ {
		r.Projects = append(r.Projects, r.Projects[0])
	}
	r.Projects = r.Projects[:15]
	for i := range r.Projects {
		r.Projects[i].Highlights = append(r.Projects[i].Highlights, strings.Repeat("long highlight text ", 12))
	}
	b, err := Render(r, "modern", "a4")
	if err != nil {
		t.Fatal(err)
	}
	checkStructure(t, b)
	if n := bytes.Count(b, []byte("/Type /Page ")); n < 2 {
		t.Fatalf("expected multiple pages, got %d", n)
	}
}

func TestEscapingAndEncoding(t *testing.T) {
	r := resume.Sample()
	r.Name = `Zoë (Test) \ Back – “Quotes” 日本`
	b, err := Render(r, "ats", "a4")
	if err != nil {
		t.Fatal(err)
	}
	text := allContent(t, b)
	if !strings.Contains(text, `Zo`+"\xeb"+` \(Test\) \\ Back `+"\x96 \x93Quotes\x94 ??") {
		t.Errorf("unexpected encoding in content stream")
	}
}

func TestWrapLongWord(t *testing.T) {
	st := Style{Regular, 10, black}
	lines := wrap(tokenize([]Run{{Text: strings.Repeat("x", 300), Style: st}}), 100, 100)
	if len(lines) < 3 {
		t.Fatalf("expected long word to be split, got %d lines", len(lines))
	}
	for _, ln := range lines {
		if w := Width(ln[0].text, Regular, 10); w > 100 {
			t.Fatalf("line too wide: %.1f", w)
		}
	}
}

func checkStructure(t *testing.T, b []byte) {
	t.Helper()
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) || !bytes.HasSuffix(b, []byte("%%EOF\n")) {
		t.Fatal("missing PDF header or trailer")
	}
	// Verify each xref offset points at the right object.
	m := regexp.MustCompile(`startxref\n(\d+)`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no startxref")
	}
	var xref int
	for _, c := range m[1] {
		xref = xref*10 + int(c-'0')
	}
	if !bytes.HasPrefix(b[xref:], []byte("xref")) {
		t.Fatal("startxref does not point at xref table")
	}
	entries := regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(b[xref:], -1)
	for i, e := range entries {
		var off int
		for _, c := range e[1] {
			off = off*10 + int(c-'0')
		}
		prefix := []byte(strings.TrimSpace(strconv.Itoa(i+1) + " 0 obj"))
		if !bytes.HasPrefix(b[off:], prefix) {
			t.Fatalf("xref entry %d points at wrong offset", i+1)
		}
	}
}

func allContent(t *testing.T, b []byte) string {
	t.Helper()
	var out strings.Builder
	re := regexp.MustCompile(`(?s)/FlateDecode >>\nstream\n(.*?)\nendstream`)
	for _, m := range re.FindAllSubmatch(b, -1) {
		zr, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(data)
	}
	return out.String()
}
