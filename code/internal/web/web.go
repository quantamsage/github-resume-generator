// Package web embeds the HTML templates and static assets.
package web

import (
	"embed"
	"html/template"
	"io/fs"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Static returns the static asset file system rooted at static/.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// Pages parses each page template together with the shared layout.
func Pages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for _, name := range []string{"landing", "analyze", "editor", "error"} {
		t, err := template.New("layout.html").ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	return pages, nil
}
