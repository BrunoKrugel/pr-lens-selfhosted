// Package webui embeds the canvas viewer page: a small bundle that fetches
// the document from /api/canvas/{id} and draws it with the official
// @coldtea/pr-lens-renderer, in dark theme, entirely client-side.
package webui

import _ "embed"

//go:embed dist/index.html
var Index []byte

//go:embed dist/viewer.js
var ViewerJS []byte

//go:embed dist/polyfills.js
var Polyfills []byte
