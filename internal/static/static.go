// Package static embeds third-party CSS, JS, and font assets served under /assets/,
// plus the default site icon (icon-64.png, credited in the page footer).
//
// To upgrade or add an asset, run scripts/download-assets.sh from the repo root,
// update the <link>/<script> tags in server/internal/tmpl/base.html and
// server/internal/tmpl/annotate.html, and remove the old versioned files.
package static

import "embed"

//go:embed *.css *.js *.png webfonts
var Files embed.FS
