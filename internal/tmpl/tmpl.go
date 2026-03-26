// Package tmpl embeds and parses HTML templates for the screenshotter server.
// Each page template is parsed together with base.html so that {{block}}
// overrides work correctly without cross-page conflicts.
package tmpl

import (
	"embed"
	"fmt"
	"html/template"
)

//go:embed *.html
var files embed.FS

// Parse returns a map from page name (e.g. "home.html") to a parsed
// *template.Template that includes base.html. Call t.ExecuteTemplate(w, "base", data).
func Parse() (map[string]*template.Template, error) {
	pages := []string{"home.html", "view.html"}
	out := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		t, err := template.New("").ParseFS(files, "base.html", page)
		if err != nil {
			return nil, fmt.Errorf("parse template %q: %w", page, err)
		}
		out[page] = t
	}
	return out, nil
}
