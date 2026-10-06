package web

import (
	"bytes"
	"html/template"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var (
	markdown  = goldmark.New(goldmark.WithExtensions(extension.GFM))
	sanitizer = newSanitizer()
)

func newSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}

// renderMarkdown turns user Markdown into sanitized HTML.
func renderMarkdown(src string) template.HTML {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(sanitizer.SanitizeBytes(buf.Bytes()))
}
