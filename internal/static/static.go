// Package static embeds third-party CSS, JS, and font assets served under /assets/,
// plus the default site icon (icon-64.png, credited in the page footer) and
// the default favicon (favicon.ico, the same icon; served at /favicon.ico
// when no favicon is configured).
//
// The third-party files carry their version in their name (the webfonts in
// their directory's), which is what lets the server mark them immutable for
// browser caches: a new version is a new URL, and the old one is never
// served with other content.
//
// To upgrade or add an asset, run scripts/download-assets.sh from the repo root,
// update the <link>/<script> tags in server/internal/tmpl/base.html and
// server/internal/tmpl/view.html, and remove the old versioned files.
package static

import "embed"

//go:embed *.css *.js *.png *.ico webfonts-*
var Files embed.FS
