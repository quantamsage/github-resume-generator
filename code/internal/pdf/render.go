package pdf

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/judie-paul/resume-generator/internal/resume"
)

// Templates lists the supported template IDs in display order.
var Templates = []string{"ats", "modern", "minimal"}

// ValidTemplate reports whether id is a known template.
func ValidTemplate(id string) bool {
	for _, t := range Templates {
		if t == id {
			return true
		}
	}
	return false
}

type theme struct {
	margin       float64
	name         Style
	headline     Style
	contact      Style
	contactLink  Style
	heading      Style
	headingUpper bool
	headingRule  bool
	ruleColor    RGB
	accentBar    bool
	accent       RGB
	body         Style
	bold         Style
	muted        Style
	bullet       string
	sectionGap   float64
	leading      float64 // multiple of body size
	linkText     bool    // print link URLs as text (ATS-friendly)
}

var (
	black = RGB{0, 0, 0}
	ink   = RGB{0.12, 0.13, 0.15}
	gray  = RGB{0.38, 0.40, 0.44}
	navy  = RGB{0.07, 0.25, 0.50}
)

func themeFor(id string) theme {
	switch id {
	case "modern":
		return theme{
			margin:      46,
			name:        Style{Bold, 24, navy},
			headline:    Style{Regular, 12, ink},
			contact:     Style{Regular, 9, gray},
			contactLink: Style{Regular, 9, navy},
			heading:     Style{Bold, 11, navy}, headingUpper: true, headingRule: true,
			ruleColor: RGB{0.75, 0.80, 0.88}, accentBar: true, accent: navy,
			body: Style{Regular, 9.8, ink}, bold: Style{Bold, 10.2, ink}, muted: Style{Italic, 9, gray},
			bullet: "•", sectionGap: 12, leading: 1.38,
		}
	case "minimal":
		return theme{
			margin:      58,
			name:        Style{Regular, 22, ink},
			headline:    Style{Regular, 11, gray},
			contact:     Style{Regular, 8.8, gray},
			contactLink: Style{Regular, 8.8, gray},
			heading:     Style{Bold, 8.5, gray}, headingUpper: true,
			body: Style{Regular, 9.6, ink}, bold: Style{Bold, 9.8, ink}, muted: Style{Regular, 8.8, gray},
			bullet: "–", sectionGap: 16, leading: 1.45,
		}
	default: // ats
		return theme{
			margin:      50,
			name:        Style{Bold, 18, black},
			headline:    Style{Regular, 11, black},
			contact:     Style{Regular, 9.5, black},
			contactLink: Style{Regular, 9.5, black},
			heading:     Style{Bold, 11, black}, headingUpper: true, headingRule: true, ruleColor: black,
			body: Style{Regular, 10, black}, bold: Style{Bold, 10, black}, muted: Style{Regular, 9.5, black},
			bullet: "•", sectionGap: 10, leading: 1.35, linkText: true,
		}
	}
}

// Render produces a PDF for the resume. The resume must already be sanitized.
func Render(r *resume.Resume, template, paper string) ([]byte, error) {
	size := A4
	if paper == "letter" {
		size = Letter
	}
	t := themeFor(template)
	doc := New(size)
	doc.Title = r.Name + " - Resume"
	l := NewLayout(doc, t.margin)

	if t.accentBar {
		doc.Rect(0, doc.H-6, doc.W, 6, t.accent)
	}

	// Header
	l.Paragraph([]Run{{Text: r.Name, Style: t.name}}, 0, t.name.Size*1.2, nil, nil)
	if r.Headline != "" {
		l.Paragraph([]Run{{Text: r.Headline, Style: t.headline}}, 0, t.headline.Size*1.45, nil, nil)
	}
	if runs := contactRuns(r.Contact, t); len(runs) > 0 {
		l.Space(2)
		l.Paragraph(runs, 0, t.contact.Size*1.5, nil, nil)
	}

	body := t.body.Size * t.leading
	section := func(title string, draw func()) {
		l.Space(t.sectionGap)
		l.Ensure(t.heading.Size*2 + body*2) // keep heading with some content
		if t.headingUpper {
			title = strings.ToUpper(title)
		}
		l.Paragraph([]Run{{Text: title, Style: t.heading}}, 0, t.heading.Size*1.3, nil, nil)
		if t.headingRule {
			l.Space(2.5)
			l.Rule(0.6, t.ruleColor)
		}
		l.Space(4)
		draw()
	}
	bullets := func(items []string) {
		for _, it := range items {
			l.Paragraph([]Run{{Text: it, Style: t.body}}, 12, body, &Run{Text: t.bullet, Style: t.body}, nil)
		}
	}

	if r.Summary != "" {
		section("Summary", func() {
			l.Paragraph([]Run{{Text: r.Summary, Style: t.body}}, 0, body, nil, nil)
		})
	}

	if len(r.Skills) > 0 {
		section("Technical Skills", func() {
			for _, g := range r.Skills {
				l.Paragraph([]Run{
					{Text: g.Category + ": ", Style: t.bold},
					{Text: strings.Join(g.Items, ", "), Style: t.body},
				}, 0, body, nil, nil)
			}
		})
	}

	if len(r.Experience) > 0 {
		section("Experience", func() {
			for i, e := range r.Experience {
				if i > 0 {
					l.Space(5)
				}
				l.Ensure(body * 3)
				head := []Run{{Text: e.Title, Style: t.bold}}
				if e.Company != "" {
					sep := ", "
					if e.Title == "" {
						sep = ""
					}
					head = append(head, Run{Text: sep + e.Company, Style: t.body})
				}
				if e.Location != "" {
					head = append(head, Run{Text: " — " + e.Location, Style: t.muted})
				}
				l.Paragraph(head, 0, body, nil, &Run{Text: dateRange(e.Start, e.End), Style: t.muted})
				bullets(e.Highlights)
			}
		})
	}

	if len(r.Projects) > 0 {
		section("Selected Projects", func() {
			for i, p := range r.Projects {
				if i > 0 {
					l.Space(5)
				}
				l.Ensure(body * 3)
				head := []Run{{Text: p.Name, Style: t.bold, Link: p.URL}}
				if len(p.Technologies) > 0 {
					head = append(head, Run{Text: " | " + strings.Join(p.Technologies, ", "), Style: t.muted})
				}
				l.Paragraph(head, 0, body, nil, nil)
				if p.Description != "" {
					l.Paragraph([]Run{{Text: p.Description, Style: t.body}}, 0, body, nil, nil)
				}
				bullets(p.Highlights)
				if links := projectLinks(p, t); len(links) > 0 {
					l.Paragraph(links, 0, t.muted.Size*1.4, nil, nil)
				}
			}
		})
	}

	if len(r.Education) > 0 {
		section("Education", func() {
			for i, e := range r.Education {
				if i > 0 {
					l.Space(4)
				}
				head := []Run{{Text: e.Degree, Style: t.bold}}
				if e.Institution != "" {
					sep := ", "
					if e.Degree == "" {
						sep = ""
					}
					head = append(head, Run{Text: sep + e.Institution, Style: t.body})
				}
				l.Paragraph(head, 0, body, nil, &Run{Text: dateRange(e.Start, e.End), Style: t.muted})
				if e.Details != "" {
					l.Paragraph([]Run{{Text: e.Details, Style: t.body}}, 0, body, nil, nil)
				}
			}
		})
	}

	if len(r.Certifications) > 0 {
		section("Certifications", func() {
			for _, c := range r.Certifications {
				runs := []Run{{Text: c.Name, Style: t.bold, Link: c.URL}}
				if c.Issuer != "" {
					runs = append(runs, Run{Text: ", " + c.Issuer, Style: t.body})
				}
				l.Paragraph(runs, 0, body, nil, &Run{Text: c.Year, Style: t.muted})
			}
		})
	}

	var buf bytes.Buffer
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("write pdf: %w", err)
	}
	return buf.Bytes(), nil
}

func contactRuns(c resume.Contact, t theme) []Run {
	type item struct{ text, link string }
	var items []item
	if c.Email != "" {
		items = append(items, item{c.Email, "mailto:" + c.Email})
	}
	if c.Phone != "" {
		items = append(items, item{c.Phone, ""})
	}
	if c.Location != "" {
		items = append(items, item{c.Location, ""})
	}
	for _, u := range []string{c.GitHub, c.LinkedIn, c.Portfolio, c.Website} {
		if u != "" {
			items = append(items, item{displayURL(u), u})
		}
	}
	var runs []Run
	for i, it := range items {
		if i > 0 {
			runs = append(runs, Run{Text: "  |  ", Style: t.contact})
		}
		st := t.contact
		if it.link != "" {
			st = t.contactLink
		}
		runs = append(runs, Run{Text: it.text, Style: st, Link: it.link})
	}
	return runs
}

func projectLinks(p resume.Project, t theme) []Run {
	var runs []Run
	add := func(label, u string) {
		if u == "" {
			return
		}
		if len(runs) > 0 {
			runs = append(runs, Run{Text: "  |  ", Style: t.muted})
		}
		text := label
		if t.linkText {
			text = displayURL(u)
		}
		runs = append(runs, Run{Text: text, Style: t.muted, Link: u})
	}
	if t.linkText {
		add("Code", p.URL)
	} else if p.URL != "" {
		add("Source: "+displayURL(p.URL), p.URL)
	}
	add("Live demo", p.DemoURL)
	return runs
}

func displayURL(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	u = strings.TrimPrefix(u, "www.")
	return strings.TrimSuffix(u, "/")
}

func dateRange(start, end string) string {
	switch {
	case start != "" && end != "":
		return start + " – " + end
	case start != "":
		return start + " – Present"
	default:
		return end
	}
}
