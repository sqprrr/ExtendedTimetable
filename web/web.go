// Package web embeds the HTML templates, static assets and icons.
package web

import "embed"

//go:embed templates
var Templates embed.FS

//go:embed static
var Static embed.FS

// Icons holds the Lucide icons (web/icons/<name>.svg) that the icon template
// inlines into pages.
//
//go:embed icons/*.svg
var Icons embed.FS
