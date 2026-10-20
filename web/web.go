// Package web embeds the HTML templates, static assets and icons.
package web

import "embed"

//go:embed templates
var Templates embed.FS

//go:embed static
var Static embed.FS

// Icons holds icons/sprite.svg, the Lucide icons that the icon template
// inlines into pages, one <symbol id="<name>"> each.
//
//go:embed icons/sprite.svg
var Icons embed.FS
