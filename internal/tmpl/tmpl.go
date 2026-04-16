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

//go:embed Roboto-Regular.ttf
var FontTTF []byte

// Parse returns a map from page name (e.g. "home.html") to a parsed
// *template.Template that includes base.html. Call t.ExecuteTemplate(w, "base", data).
func Parse() (map[string]*template.Template, error) {
	out := make(map[string]*template.Template)

	for _, page := range []string{"home.html", "view.html"} {
		t, err := template.New("").ParseFS(files, "base.html", page)
		if err != nil {
			return nil, fmt.Errorf("parse template %q: %w", page, err)
		}
		out[page] = t
	}

	// annotate.html is a standalone page that defines its own "base" block.
	t, err := template.New("").ParseFS(files, "annotate.html")
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", "annotate.html", err)
	}
	out["annotate.html"] = t

	return out, nil
}
